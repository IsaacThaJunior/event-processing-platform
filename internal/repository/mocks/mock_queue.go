package mocks

import (
	"time"

	"github.com/stretchr/testify/mock"
)

type MockQueue struct {
	mock.Mock
}

func (m *MockQueue) EnqueueWithPriority(taskID, priority string) error {
	args := m.Called(taskID, priority)
	return args.Error(0)
}

func (m *MockQueue) DequeuePriorityBlocking(timeout time.Duration) (string, string, error) {
	args := m.Called(timeout)
	return args.String(0), args.String(1), args.Error(2)
}

func (m *MockQueue) Schedule(taskID, priority string, executeAt time.Time) error {
	args := m.Called(taskID, priority, executeAt)
	return args.Error(0)
}

func (m *MockQueue) PromoteScheduled() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockQueue) EnqueueToDLQ(taskID string) error {
	args := m.Called(taskID)
	return args.Error(0)
}

func (m *MockQueue) GetQueueDepths() (map[string]int64, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]int64), args.Error(1)
}
