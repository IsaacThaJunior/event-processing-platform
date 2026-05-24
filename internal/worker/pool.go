// internal/worker/pool.go
package worker

import (
	"context"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/disintegration/imaging"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"

	"github.com/isaacthajunior/mid-prod/internal/database"
	"github.com/isaacthajunior/mid-prod/internal/domain"
	"github.com/isaacthajunior/mid-prod/internal/handler"
	"github.com/isaacthajunior/mid-prod/internal/metrics"
	"github.com/isaacthajunior/mid-prod/internal/middleware"
	"github.com/isaacthajunior/mid-prod/internal/repository"
	"github.com/isaacthajunior/mid-prod/internal/service"
	"github.com/isaacthajunior/mid-prod/internal/storage"
)

var workerTracer = otel.Tracer("worker")

type WorkerPool struct {
	queue     domain.Queue
	repo      repository.EventRepository
	workers   int
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	logger    *slog.Logger
	validator *service.TaskValidator
	storage   *storage.Client

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
	storageClient *storage.Client,
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
		storage:   storageClient,
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

	maxRetries := 5
	baseDelay := time.Second

	var lastEvent database.Event
	var task handler.TaskRequest
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		event, err := p.repo.GetEventByID(ctx, eventID)
		if err != nil {
			logCtx.AddEvent("get_event_from_db", "failed", err)
			time.Sleep(baseDelay * time.Duration(1<<(attempt-1)))
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

		// Restore trace context from the HTTP handler so this span appears in the same trace.
		// The handler serialised the W3C traceparent into task.TraceContext before saving to DB.
		spanCtx := ctx
		if task.TraceContext != "" {
			carrier := propagation.MapCarrier{"traceparent": task.TraceContext}
			spanCtx = otel.GetTextMapPropagator().Extract(ctx, carrier)
		}
		spanCtx, span := workerTracer.Start(spanCtx, "worker.process-task")
		span.SetAttributes(
			attribute.String("task.id", eventID),
			attribute.String("task.type", event.Type),
			attribute.String("task.priority", task.Priority),
			attribute.String("queue", queueName),
			attribute.Int("worker.id", workerID),
			attribute.Int("attempt", attempt),
		)

		// Keep the string trace_id in context so the logger can emit it
		ctx = context.WithValue(spanCtx, middleware.TraceIDKey, event.TraceID)

		if event.Status.String == "cancelled" {
			logCtx.AddEvent("task_cancelled", "skipped", nil)
			logCtx.Status = "cancelled"
			span.End()
			break
		}

		execStart := time.Now()
		err = p.executeTask(ctx, eventID, task, logCtx)
		metrics.TaskDuration.Observe(time.Since(execStart).Seconds())

		if err == nil {
			logCtx.AddEvent("execute_task", "success", nil)
			span.End()

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
			p.emitLog(ctx, logCtx, workerID, queueName, start)
			return
		}

		lastErr = err
		logCtx.AddEvent(fmt.Sprintf("execute_task_attempt_%d", attempt), "failed", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()

		metrics.TasksRetried.WithLabelValues(event.Type).Inc()
		if err := p.repo.LogDeliveryStatus(ctx, eventID, "retry", attempt, err.Error()); err != nil {
			logCtx.AddEvent("log_retry_status", "failed", err)
		}

		time.Sleep(baseDelay * time.Duration(1<<(attempt-1)))
	}

	// Max retries exhausted → Dead Letter Queue
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
	logCtx.Error = lastErr
	p.emitLog(ctx, logCtx, workerID, queueName, start)
}

func (p *WorkerPool) emitLog(ctx context.Context, logCtx *middleware.LogContext, workerID int, queueName string, start time.Time) {
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
	if traceID, ok := ctx.Value(middleware.TraceIDKey).(string); ok && traceID != "" {
		attrs = append(attrs, "traceID", traceID)
	}
	p.logger.Info("task processed", attrs...)
}

func (p *WorkerPool) executeTask(ctx context.Context, eventID string, task handler.TaskRequest, logCtx *middleware.LogContext) error {
	switch task.Type {
	case "resize_image":
		payload, err := injectTaskID(task.Payload, eventID)
		if err != nil {
			return err
		}
		return p.handleResizeImage(ctx, payload, logCtx)
	case "scrape_url":
		return p.handleScrapeURL(ctx, task.Payload)
	case "generate_report":
		return p.handleGenerateReport(ctx, task.Payload)
	default:
		return fmt.Errorf("unknown command: %v", task.Type)
	}
}

func injectTaskID(payload json.RawMessage, taskID string) (json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(payload, &m); err != nil {
		return nil, fmt.Errorf("injectTaskID: %w", err)
	}
	idBytes, _ := json.Marshal(taskID)
	m["task_id"] = idBytes
	return json.Marshal(m)
}

const maxImageBytes = 10 << 20 // 10 MB

func (p *WorkerPool) handleResizeImage(ctx context.Context, payload []byte, logCtx *middleware.LogContext) error {
	var params struct {
		ImageURL     string `json:"image_url"`
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		Mode         string `json:"mode"`
		OutputFormat string `json:"output_format"`
		Quality      int    `json:"quality"`
		TaskID       string `json:"task_id"`
	}
	if err := json.Unmarshal(payload, &params); err != nil {
		return fmt.Errorf("resize: parse payload: %w", err)
	}

	// Defaults
	if params.Mode == "" {
		params.Mode = "fit"
	}
	if params.OutputFormat == "" {
		params.OutputFormat = "jpeg"
	}
	if params.Quality == 0 {
		params.Quality = 85
	}

	// Download with SSRF guard and size limit
	client := safeHTTPClient(30)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, params.ImageURL, nil)
	if err != nil {
		return fmt.Errorf("resize: build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		logCtx.AddEvent("resize_download", "failed", err)
		return fmt.Errorf("resize: download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("resize: download status %d", resp.StatusCode)
		logCtx.AddEvent("resize_download", "failed", err)
		return err
	}
	logCtx.AddEvent("resize_download", "success", nil)

	limited := io.LimitReader(resp.Body, maxImageBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("resize: read body: %w", err)
	}
	if int64(len(raw)) > maxImageBytes {
		return fmt.Errorf("resize: image exceeds %d MB limit", maxImageBytes>>20)
	}

	// Decode
	src, err := imaging.Decode(bytes.NewReader(raw))
	if err != nil {
		logCtx.AddEvent("resize_decode", "failed", err)
		return fmt.Errorf("resize: decode: %w", err)
	}
	logCtx.AddEvent("resize_decode", "success", nil)

	// Resize
	var dst image.Image
	switch params.Mode {
	case "fill":
		dst = imaging.Fill(src, params.Width, params.Height, imaging.Center, imaging.Lanczos)
	case "stretch":
		dst = imaging.Resize(src, params.Width, params.Height, imaging.Lanczos)
	default: // fit
		dst = imaging.Fit(src, params.Width, params.Height, imaging.Lanczos)
	}
	logCtx.AddEvent("resize_transform", "success", nil)

	// Encode into a buffer
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
		logCtx.AddEvent("resize_encode", "failed", err)
		return fmt.Errorf("resize: encode: %w", err)
	}
	logCtx.AddEvent("resize_encode", "success", nil)

	// Upload to MinIO
	if p.storage == nil {
		return fmt.Errorf("resize: storage client not configured")
	}
	key := fmt.Sprintf("resized/%s.%s", params.TaskID, params.OutputFormat)
	if _, err := p.storage.Upload(ctx, key, contentType, &buf, int64(buf.Len())); err != nil {
		logCtx.AddEvent("resize_upload", "failed", err)
		return fmt.Errorf("resize: upload: %w", err)
	}
	logCtx.AddEvent("resize_upload", "success", nil)

	// Persist a typed result so the result endpoint knows how to serve it.
	resultJSON, _ := json.Marshal(map[string]string{"kind": "file", "key": key})
	if err := p.repo.UpdateEventResult(ctx, params.TaskID, string(resultJSON)); err != nil {
		logCtx.AddEvent("resize_save_result", "failed", err)
		return fmt.Errorf("resize: save result: %w", err)
	}
	logCtx.AddEvent("resize_save_result", "success", nil)

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
		nil,
	)
	if err != nil {
		return err
	}

	if next.ExecuteAt != nil {
		return p.queue.Schedule(nextEventID, next.Priority, *next.ExecuteAt)
	}

	return p.queue.EnqueueWithPriority(nextEventID, next.Priority)
}
