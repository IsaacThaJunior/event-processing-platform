// Package chaining implements this app's declarative task chaining: a
// submitted task can carry a "next" step that gets created and enqueued
// after the current task succeeds. pulse's worker.Pool has no concept of
// chaining — Wrap is where it lives, applied uniformly to every registered
// handler in cmd/main.go, so any task type can chain to any other.
package chaining

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/isaacthajunior/mid-prod/internal/handler"
	"github.com/isaacthajunior/mid-prod/internal/repository"
	"github.com/isaacthajunior/pulse/queue"
	"github.com/isaacthajunior/pulse/worker"
)

// Wrap runs h, and on success, creates and enqueues the task's declared
// "next" step (worker.Task.Metadata["next"], populated by
// repository.EventStore.GetTask from the request's next field), if any.
func Wrap(h worker.HandlerFunc, eventRepo repository.EventRepository, q queue.Queue) worker.HandlerFunc {
	return func(ctx context.Context, task worker.Task) error {
		if err := h(ctx, task); err != nil {
			return err
		}

		raw := task.Metadata["next"]
		if raw == "" {
			return nil
		}

		var next handler.TaskRequest
		if err := json.Unmarshal([]byte(raw), &next); err != nil {
			return fmt.Errorf("chaining: parse next: %w", err)
		}
		if next.Type == "" {
			return nil
		}

		nextID := uuid.New().String()
		payloadBytes, err := json.Marshal(next)
		if err != nil {
			return fmt.Errorf("chaining: marshal next: %w", err)
		}

		rootTaskID := task.Metadata["root_task_id"]
		if rootTaskID == "" {
			rootTaskID = task.ID
		}

		if err := eventRepo.SaveProcessedEvent(ctx, nextID, next.Type, string(payloadBytes), "pending", task.Metadata["trace_id"], next.Priority, rootTaskID, next.ExecuteAt); err != nil {
			return fmt.Errorf("chaining: save next task: %w", err)
		}

		if next.ExecuteAt != nil {
			return q.Schedule(nextID, next.Priority, *next.ExecuteAt)
		}
		return q.EnqueueWithPriority(nextID, next.Priority)
	}
}
