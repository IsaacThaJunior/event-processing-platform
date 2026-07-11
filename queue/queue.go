// Package queue defines the storage-agnostic contract a task queue backend
// must satisfy to be used by this project's worker pool and HTTP handlers.
package queue

import (
	"time"
)

// Queue is a priority task queue with delayed scheduling and a dead-letter
// queue for tasks that exhausted their retries. Implementations are free to
// choose their own backend (Redis, RabbitMQ, Kafka, ...) as long as they
// satisfy this contract.
type Queue interface {
	// EnqueueWithPriority makes taskID immediately available for dequeue,
	// ordered within its priority class ("high", "medium", or "low").
	EnqueueWithPriority(taskID, priority string) error

	// DequeuePriorityBlocking blocks up to timeout waiting for a task,
	// checking higher-priority queues first. It returns ("", "", nil) on
	// timeout with no task available.
	DequeuePriorityBlocking(timeout time.Duration) (taskID string, queueName string, err error)

	// Schedule makes taskID eligible for dequeue at executeAt rather than
	// immediately. PromoteScheduled moves it into the live queue once due.
	Schedule(taskID, priority string, executeAt time.Time) error

	// PromoteScheduled moves any scheduled tasks whose executeAt has passed
	// into their priority queue. Intended to be called periodically.
	PromoteScheduled() error

	// EnqueueToDLQ moves taskID into the dead-letter queue after it has
	// exhausted all retries.
	EnqueueToDLQ(taskID string) error

	// GetQueueDepths returns the number of pending items per named queue
	// (priority classes, scheduled, and dead-letter).
	GetQueueDepths() (map[string]int64, error)

	// GetDLQItems lists every task ID currently in the dead-letter queue.
	GetDLQItems() ([]string, error)

	// RemoveFromDLQ removes taskID from the dead-letter queue, returning an
	// error if it isn't present.
	RemoveFromDLQ(taskID string) error
}
