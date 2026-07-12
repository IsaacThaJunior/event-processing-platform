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
// via a chain (the HTTP API rejects direct submission — see task_handler.go).
//
// The caller declaring `next: {type: generate_report}` can't know
// scraped_key in advance — it's only known once the parent task (e.g.
// scrape_url) finishes running and uploads its result. So if the payload
// doesn't carry scraped_key explicitly, this handler falls back to reading
// its parent task's stored file result. That also means generate_report
// works chained after any file-producing task that stores a
// {"kind":"file","key":...} result in the same shape scrape_url uses, not
// just scrape_url specifically.
func NewGenerateReportHandler(eventRepo repository.EventRepository, storageClient *storage.Client) worker.HandlerFunc {
	return func(ctx context.Context, task worker.Task) error {
		var params struct {
			ScrapedKey string `json:"scraped_key"`
		}
		if err := json.Unmarshal(task.Payload, &params); err != nil {
			return fmt.Errorf("report: parse params: %w", err)
		}
		if storageClient == nil {
			return fmt.Errorf("report: storage client not configured")
		}

		scrapedKey := params.ScrapedKey
		if scrapedKey == "" {
			key, err := parentFileResultKey(ctx, eventRepo, task.Metadata["root_task_id"])
			if err != nil {
				return fmt.Errorf("report: scraped_key is required; submit scrape_url (or another file-producing task) with next.type=generate_report: %w", err)
			}
			scrapedKey = key
		}

		rc, err := storageClient.Download(ctx, scrapedKey)
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

// parentFileResultKey fetches parentID's event and extracts the storage
// key from its stored {"kind":"file","key":...} result.
func parentFileResultKey(ctx context.Context, eventRepo repository.EventRepository, parentID string) (string, error) {
	if parentID == "" {
		return "", fmt.Errorf("no parent task")
	}
	parent, err := eventRepo.GetEventByID(ctx, parentID)
	if err != nil {
		return "", fmt.Errorf("fetch parent task: %w", err)
	}
	if !parent.Result.Valid || parent.Result.String == "" {
		return "", fmt.Errorf("parent task %s has no result", parentID)
	}
	var result struct {
		Kind string `json:"kind"`
		Key  string `json:"key"`
	}
	if err := json.Unmarshal([]byte(parent.Result.String), &result); err != nil {
		return "", fmt.Errorf("parse parent task result: %w", err)
	}
	if result.Kind != "file" || result.Key == "" {
		return "", fmt.Errorf("parent task %s has no usable file result", parentID)
	}
	return result.Key, nil
}
