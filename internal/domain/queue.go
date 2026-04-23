package domain

import (
	"time"
)

type Queue interface {
	EnqueueWithPriority(taskID, priority string) error
	DequeuePriorityBlocking(timeout time.Duration) (string, string, error)
	Schedule(taskID, priority string, executeAt time.Time) error
	PromoteScheduled() error
	EnqueueToDLQ(taskID string) error
	GetQueueDepths() (map[string]int64, error)
	GetDLQItems() ([]string, error)
	RemoveFromDLQ(taskID string) error
}
