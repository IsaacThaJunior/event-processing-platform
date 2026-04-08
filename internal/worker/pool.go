// internal/worker/pool.go
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/isaacthajunior/mid-prod/internal/database"
	"github.com/isaacthajunior/mid-prod/internal/domain"
	"github.com/isaacthajunior/mid-prod/internal/metrics"
	"github.com/isaacthajunior/mid-prod/internal/repository"
)

type WorkerPool struct {
	queue   domain.Queue
	repo    repository.EventRepository
	workers int
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	logger  *slog.Logger
}

func NewWorkerPool(
	queue domain.Queue,
	eventRepo repository.EventRepository,
	workerCount int,
	logger *slog.Logger,
) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())
	return &WorkerPool{
		queue:   queue,
		repo:    eventRepo,
		workers: workerCount,
		ctx:     ctx,
		cancel:  cancel,
		logger:  logger,
	}
}

func (p *WorkerPool) Start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
}

func (p *WorkerPool) worker(id int) {
	defer p.wg.Done()
	p.logger.Info("Worker Started", "worker_id", id)

	for {
		select {
		case <-p.ctx.Done():
			fmt.Printf("Worker %d stopping\n", id)
			return
		default:
			// Dequeue returns the event ID (not JSON anymore)
			eventID, err := p.queue.Dequeue()
			if err != nil {
				fmt.Println("Dequeue error:", err)
				time.Sleep(1 * time.Second)
				continue
			}
			if eventID == "" {
				time.Sleep(500 * time.Millisecond)
				continue
			}

			fmt.Printf("Worker %d processing task: %s\n", id, eventID)
			p.processWithRetry(eventID, id)
		}
	}
}

func (p *WorkerPool) Stop() {
	p.cancel()
	p.wg.Wait()
}

func (p *WorkerPool) processWithRetry(eventID string, workerID int) {
	maxRetries := 5
	baseDelay := time.Second

	var lastEvent database.Event

	for attempt := 1; attempt <= maxRetries; attempt++ {
		ctx := context.Background()

		// Get the event from database to get all metadata
		event, err := p.repo.GetEventByID(ctx, eventID)
		if err != nil {
			p.logger.Error("Failed to get event from DB",
				"event_id", eventID,
				"attempt", attempt,
				"error", err,
			)
			backoff := baseDelay * time.Duration(1<<(attempt-1))
			time.Sleep(backoff)
			continue
		}

		lastEvent = event

		// Execute the actual job based on command
		start := time.Now()
		err = p.executeJob(ctx, event)
		duration := time.Since(start).Seconds()
		metrics.TaskDuration.Observe(duration)

		if err == nil {
			fmt.Printf("Worker %d successfully processed task %s\n", workerID, eventID)
			p.logger.Info("processing task successful",
				"worker_id", workerID,
				"task_id", eventID,
				"type", event.Type,
				"trace_id", event.TraceID,
			)

			_ = p.repo.UpdateEventStatus(ctx, eventID, "processed")
			metrics.TasksProcessed.WithLabelValues(event.Type).Inc()
			_ = p.repo.LogDeliveryStatus(ctx, eventID, "processed", attempt, "")
			return
		}

		// Log retry attempt
		p.logger.Error("failed to process task",
			"task_id", eventID,
			"type", event.Type,
			"attempt", attempt,
			"error", err,
		)
		metrics.TasksRetried.WithLabelValues(event.Type).Inc()
		_ = p.repo.LogDeliveryStatus(ctx, eventID, "retry", attempt, err.Error())

		backoff := baseDelay * time.Duration(1<<(attempt-1))
		fmt.Printf("Worker %d retrying task %s in %v\n", workerID, eventID, backoff)
		time.Sleep(backoff)
	}

	// After max retries → Dead Letter Queue
	fmt.Printf("Task %s moved to DLQ\n", eventID)
	metrics.TasksFailed.WithLabelValues(lastEvent.Type).Inc()
	_ = p.repo.UpdateEventStatus(context.Background(), eventID, "failed")
	_ = p.repo.LogDeliveryStatus(context.Background(), eventID, "failed", maxRetries, "max retries exceeded")
	_ = p.queue.EnqueueToDLQ(eventID)
	p.logger.Info("Moving task to Dead letter Queue",
		"task_id", eventID,
	)
}

func (p *WorkerPool) executeJob(ctx context.Context, event database.Event) error {
	// Route to the appropriate job handler based on command

	switch event.Type {
	case "resize_image":
		return p.handleResizeImage(ctx, event)
	case "scrape_url":
		return p.handleScrapeURL(ctx, event)
	case "generate_report":
		return p.handleGenerateReport(ctx, event)
	default:
		return fmt.Errorf("unknown command: %v", event.Type)
	}
}

func (p *WorkerPool) handleResizeImage(ctx context.Context, event database.Event) error {
	// Parse payload
	var params struct {
		ImageURL string `json:"image_url"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
	}
	if err := json.Unmarshal([]byte(event.Payload), &params); err != nil {
		return fmt.Errorf("failed to parse resize params: %w", err)
	}

	// Validate parameters
	if params.ImageURL == "" {
		return fmt.Errorf("image_url is required")
	}

	fmt.Printf("📷 [Job] Resizing image %s to %dx%d \n",
		params.ImageURL, params.Width, params.Height)

	// TODO: Implement actual image resizing logic here
	time.Sleep(1 * time.Second)

	return nil
}

func (p *WorkerPool) handleScrapeURL(ctx context.Context, event database.Event) error {
	var params struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(event.Payload), &params); err != nil {
		return fmt.Errorf("failed to parse scrape params: %w", err)
	}

	if params.URL == "" {
		return fmt.Errorf("url is required")
	}

	fmt.Printf("🔍 [Job] Scraping URL %s \n", params.URL)

	// TODO: Implement actual URL scraping logic here

	return nil
}

func (p *WorkerPool) handleGenerateReport(ctx context.Context, event database.Event) error {
	var params struct {
		Date string `json:"date"`
	}
	if err := json.Unmarshal([]byte(event.Payload), &params); err != nil {
		return fmt.Errorf("failed to parse report params: %w", err)
	}

	fmt.Printf("📊 [Job] Generating report for date %s \n", params.Date)

	// TODO: Implement actual report generation logic here

	return nil
}
