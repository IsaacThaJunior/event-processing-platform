// internal/worker/pool.go
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/isaacthajunior/mid-prod/internal/database"
	"github.com/isaacthajunior/mid-prod/internal/domain"
	"github.com/isaacthajunior/mid-prod/internal/handler"
	"github.com/isaacthajunior/mid-prod/internal/metrics"
	"github.com/isaacthajunior/mid-prod/internal/middleware"
	"github.com/isaacthajunior/mid-prod/internal/repository"
	"github.com/isaacthajunior/mid-prod/internal/service"
)

type WorkerPool struct {
	queue     domain.Queue
	repo      repository.EventRepository
	workers   int
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	logger    *slog.Logger
	validator *service.TaskValidator

	activeWorkers  atomic.Int32
	totalProcessed atomic.Int64
	totalFailed    atomic.Int64
	startTime      time.Time
}

func NewWorkerPool(
	queue domain.Queue,
	eventRepo repository.EventRepository,
	workerCount int,
	logger *slog.Logger,
	validator *service.TaskValidator,
) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())
	return &WorkerPool{
		queue:     queue,
		repo:      eventRepo,
		workers:   workerCount,
		ctx:       ctx,
		cancel:    cancel,
		logger:    logger,
		validator: validator,
		startTime: time.Now(),
	}
}

func (p *WorkerPool) HealthStats() domain.WorkerHealthStats {
	active := p.activeWorkers.Load()
	return domain.WorkerHealthStats{
		TotalWorkers:   p.workers,
		ActiveWorkers:  active,
		IdleWorkers:    int32(p.workers) - active,
		TotalProcessed: p.totalProcessed.Load(),
		TotalFailed:    p.totalFailed.Load(),
		UptimeSeconds:  int64(time.Since(p.startTime).Seconds()),
	}
}

func (p *WorkerPool) Start() {
	p.wg.Add(1)
	go p.scheduler()

	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
}

func (p *WorkerPool) scheduler() {
	defer p.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			if err := p.queue.PromoteScheduled(); err != nil {
				p.logger.Debug("scheduler: failed to promote scheduled tasks", "error", err)
			}
		}
	}
}

func (p *WorkerPool) worker(id int) {
	defer p.wg.Done()
	p.logger.Debug("Worker Started", "worker_id", id)

	for {
		select {
		case <-p.ctx.Done():
			p.logger.Debug("Worker %d stopping\n", "id", id)
			return
		default:
			eventID, queueName, err := p.queue.DequeuePriorityBlocking(5 * time.Second)
			if err != nil {
				p.logger.Debug("failed to dequeue", "error", err)
				continue
			}

			if eventID == "" {
				continue
			}

			p.processWithRetry(eventID, id, queueName)
		}
	}
}

func (p *WorkerPool) Stop() {
	p.cancel()
	p.wg.Wait()
}

func (p *WorkerPool) processWithRetry(eventID string, workerID int, queueName string) {
	p.activeWorkers.Add(1)
	defer p.activeWorkers.Add(-1)

	ctx, logCtx := middleware.WithLogContext(context.Background())

	logCtx.EventID = eventID
	logCtx.WorkerID = &workerID

	start := time.Now()
	defer func() {
		attrs := []any{
			"task_id", logCtx.EventID,
			"worker_id", workerID,
			"queue", queueName,
			slog.Duration("duration", time.Since(start)),
		}
		if logCtx.TaskType != "" {
			attrs = append(attrs, "task_type", logCtx.TaskType)
		}
		if logCtx.Priority != "" {
			attrs = append(attrs, "priority", logCtx.Priority)
		}
		if logCtx.Status != "" {
			attrs = append(attrs, "status", logCtx.Status)
		}
		if len(logCtx.Events) > 0 {
			attrs = append(attrs, "events", logCtx.Events)
		}
		if logCtx.Error != nil {
			attrs = append(attrs, "error", logCtx.Error)
		}
		if traceID, ok := ctx.Value(middleware.TraceIDKey).(string); ok {
			attrs = append(attrs, "trace_id", traceID)
		}
		p.logger.Info("task processed successfully", attrs...)
	}()

	maxRetries := 5
	baseDelay := time.Second

	var lastEvent database.Event
	var task handler.TaskRequest

	for attempt := 1; attempt <= maxRetries; attempt++ {
		event, err := p.repo.GetEventByID(ctx, eventID)
		ctx = context.WithValue(ctx, middleware.TraceIDKey, event.TraceID)
		if err != nil {
			logCtx.AddEvent("get_event_from_db", "failed", err)
			backoff := baseDelay * time.Duration(1<<(attempt-1))
			time.Sleep(backoff)
			continue
		}

		if err := json.Unmarshal([]byte(event.Payload), &task); err != nil {
			logCtx.AddEvent("unmarshal_payload", "failed", err)
			logCtx.Status = "failed"
			return
		}

		logCtx.TaskType = event.Type
		logCtx.Priority = task.Priority
		lastEvent = event

		if event.Status.String == "cancelled" {
			logCtx.AddEvent("task_cancelled", "skipped", nil)
			logCtx.Status = "cancelled"
			return
		}

		execStart := time.Now()
		err = p.executeTask(ctx, task)
		metrics.TaskDuration.Observe(time.Since(execStart).Seconds())

		if err == nil {
			logCtx.AddEvent("execute_task", "success", nil)

			if err := p.repo.UpdateEventStatus(ctx, eventID, "processed"); err != nil {
				logCtx.AddEvent("update_status_processed", "failed", err)
			}
			p.totalProcessed.Add(1)
			metrics.TasksProcessed.WithLabelValues(event.Type).Inc()

			if err := p.repo.LogDeliveryStatus(ctx, eventID, "processed", attempt, ""); err != nil {
				logCtx.AddEvent("log_delivery_status", "failed", err)
			}

			if task.Next != nil {
				if err := p.enqueueNextTask(ctx, event, task.Next, event.TraceID); err != nil {
					logCtx.AddEvent("enqueue_next_task", "failed", err)
				} else {
					logCtx.AddEvent("enqueue_next_task", "success", nil)
				}
			}

			logCtx.Status = "processed"
			return
		}

		logCtx.AddEvent(fmt.Sprintf("execute_task_attempt_%d", attempt), "failed", err)
		metrics.TasksRetried.WithLabelValues(event.Type).Inc()
		if err := p.repo.LogDeliveryStatus(ctx, eventID, "retry", attempt, err.Error()); err != nil {
			logCtx.AddEvent("log_retry_status", "failed", err)
		}

		backoff := baseDelay * time.Duration(1<<(attempt-1))
		time.Sleep(backoff)
	}

	// After max retries → Dead Letter Queue
	p.totalFailed.Add(1)
	metrics.TasksFailed.WithLabelValues(lastEvent.Type).Inc()

	if err := p.repo.UpdateEventStatus(ctx, eventID, "failed"); err != nil {
		logCtx.AddEvent("update_status_failed", "failed", err)
	}
	if err := p.repo.LogDeliveryStatus(ctx, eventID, "failed", maxRetries, "max retries exceeded"); err != nil {
		logCtx.AddEvent("log_final_failure", "failed", err)
	}
	if err := p.queue.EnqueueToDLQ(eventID); err != nil {
		logCtx.AddEvent("enqueue_dlq", "failed", err)
	} else {
		logCtx.AddEvent("enqueue_dlq", "success", nil)
	}

	logCtx.Status = "failed"
}

func (p *WorkerPool) executeTask(ctx context.Context, task handler.TaskRequest) error {
	switch task.Type {
	case "resize_image":
		return p.handleResizeImage(ctx, task.Payload)
	case "scrape_url":
		return p.handleScrapeURL(ctx, task.Payload)
	case "generate_report":
		return p.handleGenerateReport(ctx, task.Payload)
	default:
		return fmt.Errorf("unknown command: %v", task.Type)
	}
}

func (p *WorkerPool) handleResizeImage(ctx context.Context, payload []byte) error {
	// Parse payload
	var params struct {
		ImageURL string `json:"image_url"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
	}
	if err := json.Unmarshal([]byte(payload), &params); err != nil {
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

func (p *WorkerPool) handleScrapeURL(ctx context.Context, payload []byte) error {
	var params struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(payload), &params); err != nil {
		return fmt.Errorf("failed to parse scrape params: %w", err)
	}

	if params.URL == "" {
		return fmt.Errorf("url is required")
	}

	fmt.Printf("🔍 [Job] Scraping URL %s \n", params.URL)

	// TODO: Implement actual URL scraping logic here

	return nil
}

func (p *WorkerPool) handleGenerateReport(ctx context.Context, payload []byte) error {
	var params struct {
		Date string `json:"date"`
	}
	if err := json.Unmarshal([]byte(payload), &params); err != nil {
		return fmt.Errorf("failed to parse report params: %w", err)
	}

	fmt.Printf("📊 [Job] Generating report for date %s \n", params.Date)

	// TODO: Implement actual report generation logic here

	return nil
}

func (p *WorkerPool) enqueueNextTask(
	ctx context.Context,
	parent database.Event,
	next *handler.TaskRequest,
	traceID string,
) error {

	nextEventID := uuid.New().String()

	payloadBytes, _ := json.Marshal(next)

	// All tasks in a chain share the same parentID (the root/first task).
	// If this parent was itself a child, its parentID already points to root.
	rootTaskID := parent.ID
	if parent.Parentid.Valid && parent.Parentid.String != "" {
		rootTaskID = parent.Parentid.String
	}

	err := p.repo.SaveProcessedEvent(
		ctx,
		nextEventID,
		next.Type,
		string(payloadBytes),
		"pending",
		traceID,
		next.Priority,
		rootTaskID,
	)
	if err != nil {
		return err
	}

	if next.ExecuteAt != nil {
		return p.queue.Schedule(nextEventID, next.Priority, *next.ExecuteAt)
	}

	return p.queue.EnqueueWithPriority(nextEventID, next.Priority)
}
