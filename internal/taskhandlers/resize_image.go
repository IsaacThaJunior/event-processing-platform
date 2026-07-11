package taskhandlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"strings"

	"github.com/disintegration/imaging"

	"github.com/isaacthajunior/mid-prod/internal/repository"
	"github.com/isaacthajunior/mid-prod/internal/storage"
	"github.com/isaacthajunior/pulse/worker"
)

const maxImageBytes = 10 << 20 // 10 MB

// NewResizeImageHandler downloads an image (guarded against SSRF), resizes
// it, uploads the result to storage, and records the result key against
// the task.
func NewResizeImageHandler(eventRepo repository.EventRepository, storageClient *storage.Client) worker.HandlerFunc {
	return func(ctx context.Context, task worker.Task) error {
		var params struct {
			ImageURL     string `json:"image_url"`
			Width        int    `json:"width"`
			Height       int    `json:"height"`
			Mode         string `json:"mode"`
			OutputFormat string `json:"output_format"`
			Quality      int    `json:"quality"`
		}
		if err := json.Unmarshal(task.Payload, &params); err != nil {
			return fmt.Errorf("resize: parse payload: %w", err)
		}

		if params.Mode == "" {
			params.Mode = "fit"
		}
		if params.OutputFormat == "" {
			params.OutputFormat = "jpeg"
		}
		if params.Quality == 0 {
			params.Quality = 85
		}

		client := safeHTTPClient(30)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, params.ImageURL, nil)
		if err != nil {
			return fmt.Errorf("resize: build request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("resize: download: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("resize: download status %d", resp.StatusCode)
		}

		limited := io.LimitReader(resp.Body, maxImageBytes+1)
		raw, err := io.ReadAll(limited)
		if err != nil {
			return fmt.Errorf("resize: read body: %w", err)
		}
		if int64(len(raw)) > maxImageBytes {
			return fmt.Errorf("resize: image exceeds %d MB limit", maxImageBytes>>20)
		}

		src, err := imaging.Decode(bytes.NewReader(raw))
		if err != nil {
			return fmt.Errorf("resize: decode: %w", err)
		}

		var dst image.Image
		switch params.Mode {
		case "fill":
			dst = imaging.Fill(src, params.Width, params.Height, imaging.Center, imaging.Lanczos)
		case "stretch":
			dst = imaging.Resize(src, params.Width, params.Height, imaging.Lanczos)
		default: // fit
			dst = imaging.Fit(src, params.Width, params.Height, imaging.Lanczos)
		}

		var buf bytes.Buffer
		var contentType string
		switch strings.ToLower(params.OutputFormat) {
		case "png":
			contentType = "image/png"
			err = imaging.Encode(&buf, dst, imaging.PNG)
		default: // jpeg
			contentType = "image/jpeg"
			err = imaging.Encode(&buf, dst, imaging.JPEG, imaging.JPEGQuality(params.Quality))
		}
		if err != nil {
			return fmt.Errorf("resize: encode: %w", err)
		}

		if storageClient == nil {
			return fmt.Errorf("resize: storage client not configured")
		}
		key := fmt.Sprintf("resized/%s.%s", task.ID, params.OutputFormat)
		if _, err := storageClient.Upload(ctx, key, contentType, &buf, int64(buf.Len())); err != nil {
			return fmt.Errorf("resize: upload: %w", err)
		}

		resultJSON, _ := json.Marshal(map[string]string{"kind": "file", "key": key})
		if err := eventRepo.UpdateEventResult(ctx, task.ID, string(resultJSON)); err != nil {
			return fmt.Errorf("resize: save result: %w", err)
		}

		return nil
	}
}
