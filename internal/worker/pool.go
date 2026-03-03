package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/isaacthajunior/mid-prod/internal/domain"
	"github.com/isaacthajunior/mid-prod/internal/repository"
)

type WorkerPool struct {
	queue   domain.Queue
	repo    repository.EventRepository
	workers int
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewWorkerPool(queue domain.Queue, repo repository.EventRepository, workerCount int) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())
	return &WorkerPool{
		queue:   queue,
		repo:    repo,
		workers: workerCount,
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (p *WorkerPool) Start() {
	for i := 0; i < p.workers; i++ {
		go p.worker(i)
	}
}

func (p *WorkerPool) worker(id int) {
	fmt.Printf("Worker %d started\n", id)
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
			payload := fmt.Sprintf("processed by worker %d", id)
			eventType := "processed"
			ctx := context.Background()
			err = p.repo.SaveProcessedEvent(ctx, taskID, eventType, payload)
			if err != nil {
				fmt.Printf("Worker %d failed to save event: %v\n", id, err)
			} else {
				fmt.Printf("Worker %d processed and saved task %s\n", id, taskID)
			}
		}
	}
}

func (p *WorkerPool) Stop() {
	p.cancel()
}
