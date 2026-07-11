package taskhandlers

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"

	"github.com/isaacthajunior/mid-prod/internal/repository"
	"github.com/isaacthajunior/mid-prod/internal/storage"
	"github.com/isaacthajunior/pulse/worker"
)

// NewGenerateReportHandler downloads the scraped data referenced by
// scraped_key, renders it as CSV, and uploads the report. Only reachable
// via the scrape_url → generate_report chain; the HTTP API rejects direct
// submission (see task_handler.go).
func NewGenerateReportHandler(eventRepo repository.EventRepository, storageClient *storage.Client) worker.HandlerFunc {
	return func(ctx context.Context, task worker.Task) error {
		var params struct {
			ScrapedKey string `json:"scraped_key"`
		}
		if err := json.Unmarshal(task.Payload, &params); err != nil {
			return fmt.Errorf("report: parse params: %w", err)
		}
		if params.ScrapedKey == "" {
			return fmt.Errorf("report: scraped_key is required; submit scrape_url with next.type=generate_report")
		}
		if storageClient == nil {
			return fmt.Errorf("report: storage client not configured")
		}

		rc, err := storageClient.Download(ctx, params.ScrapedKey)
		if err != nil {
			return fmt.Errorf("report: download scraped data: %w", err)
		}
		defer rc.Close()

		var page scrapedPage
		if err := json.NewDecoder(rc).Decode(&page); err != nil {
			return fmt.Errorf("report: decode scraped data: %w", err)
		}

		var buf bytes.Buffer
		w := csv.NewWriter(&buf)
		_ = w.Write([]string{"section", "content"})
		_ = w.Write([]string{"url", page.URL})
		_ = w.Write([]string{"title", page.Title})
		_ = w.Write([]string{"scraped_at", page.ScrapedAt})
		for _, h := range page.Headings {
			_ = w.Write([]string{"heading", h})
		}
		for _, para := range page.Paragraphs {
			_ = w.Write([]string{"paragraph", para})
		}
		for _, link := range page.Links {
			_ = w.Write([]string{"link", link})
		}
		w.Flush()
		if err := w.Error(); err != nil {
			return fmt.Errorf("report: write csv: %w", err)
		}

		key := fmt.Sprintf("reports/%s.csv", task.ID)
		if _, err := storageClient.Upload(ctx, key, "text/csv", &buf, int64(buf.Len())); err != nil {
			return fmt.Errorf("report: upload: %w", err)
		}

		resultJSON, _ := json.Marshal(map[string]string{"kind": "file", "key": key})
		if err := eventRepo.UpdateEventResult(ctx, task.ID, string(resultJSON)); err != nil {
			return fmt.Errorf("report: save result: %w", err)
		}

		return nil
	}
}
