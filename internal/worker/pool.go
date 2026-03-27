package worker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

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

func NewWorkerPool(queue domain.Queue, repo repository.EventRepository, workerCount int, logger *slog.Logger) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())
	return &WorkerPool{
		queue:   queue,
		repo:    repo,
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
	p.logger.Info("Worker Started",
		"worker_id", id,
	)
	for {
		select {
		case <-p.ctx.Done():
			fmt.Printf("Worker %d stopping\n", id)
			return
		default:
			taskID, err := p.queue.Dequeue()
			if err != nil {
				fmt.Println("Dequeue error:", err)
				time.Sleep(1 * time.Second)
				continue
			}
			if taskID == "" {
				time.Sleep(500 * time.Millisecond)
				continue
			}

			// TODO: process task
			fmt.Printf("Worker %d processing task: %s\n", id, taskID)
			p.processWithRetry(taskID, id)
		}
	}
}

func (p *WorkerPool) Stop() {
	p.cancel()
	p.wg.Wait() // Added this for graceful shutdown. It waits for workers to finish
}

func (p *WorkerPool) processWithRetry(taskID string, workerID int) {
	maxRetries := 5
	baseDelay := time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		ctx := context.Background()

		err := p.repo.SaveProcessedEvent(ctx, taskID, "generic", fmt.Sprintf("processed by worker %d", workerID))

		if err == nil {
			fmt.Printf("Worker %d successfully processed task %s\n", workerID, taskID)
			p.logger.Info("processing task",
				"worker_id", workerID,
				"task_id", taskID,
			)

			// ✅ Log success
			atomic.AddInt64(&metrics.TotalProcessed, 1)
			_ = p.repo.LogDeliveryStatus(ctx, taskID, "processed", attempt, "")

			return
		}

		// ❌ Log retry attempt
		p.logger.Error("failed to process task",
			"task_id", taskID,
			"attempt", attempt,
			"error", err,
		)
		atomic.AddInt64(&metrics.TotalRetried, 1)
		_ = p.repo.LogDeliveryStatus(ctx, taskID, "retry", attempt, err.Error())

		backoff := baseDelay * time.Duration(1<<(attempt-1))
		fmt.Printf("Worker %d retrying task %s in %v\n", workerID, taskID, backoff)

		time.Sleep(backoff)
	}

	// 🚨 After max retries → Dead Letter Queue
	fmt.Printf("Task %s moved to DLQ\n", taskID)
	atomic.AddInt64(&metrics.TotalFailed, 1)

	_ = p.queue.EnqueueToDLQ(taskID)
	_ = p.repo.LogDeliveryStatus(context.Background(), taskID, "failed", maxRetries, "max retries exceeded")
	p.logger.Info("Moving task to Dead letter Queue",
		"task_id", taskID,
	)
}
