package taskhandlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/html"

	"github.com/isaacthajunior/mid-prod/internal/repository"
	"github.com/isaacthajunior/mid-prod/internal/storage"
	"github.com/isaacthajunior/pulse/queue"
	"github.com/isaacthajunior/pulse/worker"
)

type scrapedPage struct {
	URL        string   `json:"url"`
	Title      string   `json:"title"`
	Headings   []string `json:"headings"`
	Paragraphs []string `json:"paragraphs"`
	Links      []string `json:"links"`
	ScrapedAt  string   `json:"scraped_at"`
}

// NewScrapeURLHandler fetches a page (guarded against SSRF), extracts its
// text content, uploads the result to storage, and — on success — chains
// straight into a generate_report task built from the scraped data. The
// chain is ordinary handler code: the pool has no concept of chaining.
func NewScrapeURLHandler(eventRepo repository.EventRepository, q queue.Queue, storageClient *storage.Client) worker.HandlerFunc {
	return func(ctx context.Context, task worker.Task) error {
		var params struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(task.Payload, &params); err != nil {
			return fmt.Errorf("scrape: parse params: %w", err)
		}
		if params.URL == "" {
			return fmt.Errorf("scrape: url is required")
		}
		if storageClient == nil {
			return fmt.Errorf("scrape: storage client not configured")
		}

		resp, err := safeHTTPClient(30).Get(params.URL)
		if err != nil {
			return fmt.Errorf("scrape: fetch %s: %w", params.URL, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("scrape: unexpected status %d for %s", resp.StatusCode, params.URL)
		}

		doc, err := html.Parse(io.LimitReader(resp.Body, 5<<20))
		if err != nil {
			return fmt.Errorf("scrape: parse html: %w", err)
		}

		page := extractPage(doc, params.URL)
		data, err := json.Marshal(page)
		if err != nil {
			return fmt.Errorf("scrape: marshal: %w", err)
		}

		key := fmt.Sprintf("scraped/%s.json", task.ID)
		if _, err := storageClient.Upload(ctx, key, "application/json", bytes.NewReader(data), int64(len(data))); err != nil {
			return fmt.Errorf("scrape: upload: %w", err)
		}

		resultJSON, _ := json.Marshal(map[string]string{"kind": "file", "key": key})
		if err := eventRepo.UpdateEventResult(ctx, task.ID, string(resultJSON)); err != nil {
			return fmt.Errorf("scrape: save result: %w", err)
		}

		// Chain: automatically create and enqueue generate_report using the
		// data we just scraped.
		nextID := uuid.New().String()
		nextPayload, _ := json.Marshal(map[string]string{"scraped_key": key})
		traceID := task.Metadata["trace_id"]
		rootTaskID := task.Metadata["root_task_id"]
		if rootTaskID == "" {
			rootTaskID = task.ID
		}
		if err := eventRepo.SaveProcessedEvent(ctx, nextID, "generate_report", string(nextPayload), "pending", traceID, task.Priority, rootTaskID, nil); err != nil {
			return fmt.Errorf("scrape: save next task: %w", err)
		}
		if err := q.EnqueueWithPriority(nextID, task.Priority); err != nil {
			return fmt.Errorf("scrape: enqueue next task: %w", err)
		}

		return nil
	}
}

// extractPage walks an HTML document and pulls out title, headings, paragraphs, and links.
func extractPage(doc *html.Node, pageURL string) scrapedPage {
	page := scrapedPage{URL: pageURL, ScrapedAt: time.Now().UTC().Format(time.RFC3339)}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style":
				return
			case "title":
				if n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
					page.Title = strings.TrimSpace(n.FirstChild.Data)
				}
			case "h1", "h2", "h3":
				if text := extractText(n); text != "" {
					page.Headings = append(page.Headings, text)
				}
			case "p":
				text := extractText(n)
				if len(text) >= 20 {
					if len(text) > 300 {
						text = text[:300]
					}
					page.Paragraphs = append(page.Paragraphs, text)
				}
			case "a":
				for _, attr := range n.Attr {
					if attr.Key == "href" && strings.HasPrefix(attr.Val, "http") {
						page.Links = append(page.Links, attr.Val)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return page
}

func extractText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}
