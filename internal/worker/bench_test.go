package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/isaacthajunior/mid-prod/internal/database"
	"github.com/isaacthajunior/mid-prod/internal/handler"
	"github.com/isaacthajunior/mid-prod/internal/repository"
	"github.com/isaacthajunior/mid-prod/internal/service"
	"github.com/jackc/pgx/v5/pgtype"
)

// --- stubs (no testify: reflection overhead pollutes alloc counts) ---

type benchRepo struct {
	events map[string]database.Event
}

func (r *benchRepo) GetEventByID(_ context.Context, id string) (database.Event, error) {
	return r.events[id], nil
}
func (r *benchRepo) SaveProcessedEvent(_ context.Context, id, eventType, payload, status, traceID, priority, parentID string) error {
	return nil
}
func (r *benchRepo) ListProcessedEvents(_ context.Context) ([]database.Event, error) { return nil, nil }
func (r *benchRepo) LogDeliveryStatus(_ context.Context, id, status string, attempt int, errMsg string) error {
	return nil
}
func (r *benchRepo) UpdateEventStatus(_ context.Context, id, status string) error { return nil }
func (r *benchRepo) CancelTask(_ context.Context, id string) error                { return nil }

type benchQueue struct{}

func (q *benchQueue) EnqueueWithPriority(taskID, priority string) error { return nil }
func (q *benchQueue) DequeuePriorityBlocking(_ time.Duration) (string, string, error) {
	return "", "", nil
}
func (q *benchQueue) Schedule(taskID, priority string, _ time.Time) error { return nil }
func (q *benchQueue) PromoteScheduled() error                             { return nil }
func (q *benchQueue) EnqueueToDLQ(taskID string) error                    { return nil }
func (q *benchQueue) GetQueueDepths() (map[string]int64, error)           { return nil, nil }

// --- helpers ---

func newBenchPool(repo repository.EventRepository) *WorkerPool {
	ctx, cancel := newWorkerCtx()
	return &WorkerPool{
		queue:     &benchQueue{},
		repo:      repo,
		workers:   1,
		ctx:       ctx,
		cancel:    cancel,
		logger:    slog.Default(),
		validator: service.NewTaskValidator(),
	}
}

func newWorkerCtx() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

func seedEvent(repo *benchRepo, taskType string, rawPayload json.RawMessage) string {
	id := uuid.New().String()
	req := handler.TaskRequest{Type: taskType, Payload: rawPayload, Priority: "medium"}
	full, _ := json.Marshal(req)
	repo.events[id] = database.Event{
		ID:      id,
		Type:    taskType,
		Payload: string(full),
		Status:  pgtype.Text{String: "pending", Valid: true},
		TraceID: "bench",
	}
	return id
}

// --- benchmarks ---

// BenchmarkExecuteTask measures routing + handler overhead per task type.
func BenchmarkExecuteTask(b *testing.B) {
	pool := newBenchPool(&benchRepo{events: map[string]database.Event{}})
	ctx := context.Background()

	cases := []struct {
		name string
		task handler.TaskRequest
	}{
		{
			"resize_image",
			handler.TaskRequest{
				Type:    "resize_image",
				Payload: json.RawMessage(`{"image_url":"https://picsum.photos/300","width":600,"height":400}`),
			},
		},
		{
			"scrape_url",
			handler.TaskRequest{
				Type:    "scrape_url",
				Payload: json.RawMessage(`{"url":"https://example.com"}`),
			},
		},
		{
			"generate_report",
			handler.TaskRequest{
				Type:    "generate_report",
				Payload: json.RawMessage(`{"date":"2026-01-01"}`),
			},
		},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				pool.executeTask(ctx, tc.task)
			}
		})
	}
}

// BenchmarkProcessWithRetry measures the full single-task pipeline:
// DB fetch → cancelled check → execute → status update → delivery log.
func BenchmarkProcessWithRetry(b *testing.B) {
	repo := &benchRepo{events: make(map[string]database.Event, b.N)}
	pool := newBenchPool(repo)

	ids := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		ids[i] = seedEvent(repo, "scrape_url", json.RawMessage(`{"url":"https://example.com"}`))
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		pool.processWithRetry(ids[i], 0)
	}
}

// BenchmarkProcessWithRetryParallel measures throughput with GOMAXPROCS goroutines
// competing on the processing path — mirrors a real multi-worker pool.
func BenchmarkProcessWithRetryParallel(b *testing.B) {
	const poolSize = 10_000
	repo := &benchRepo{events: make(map[string]database.Event, poolSize)}
	pool := newBenchPool(repo)

	ids := make([]string, poolSize)
	for i := range ids {
		ids[i] = seedEvent(repo, "scrape_url", json.RawMessage(`{"url":"https://example.com"}`))
	}

	var idx atomic.Int64
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			i := idx.Add(1) % poolSize
			pool.processWithRetry(ids[i], 0)
		}
	})
}
