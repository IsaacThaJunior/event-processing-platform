package mocks

import (
	"github.com/stretchr/testify/mock"
)

type MockQueue struct {
	mock.Mock
}

func (m *MockQueue) Enqueue(taskID string) error {
	args := m.Called(taskID)
	return args.Error(0)
}

func (m *MockQueue) Dequeue() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *MockQueue) EnqueueToDLQ(taskID string) error {
	args := m.Called(taskID)
	return args.Error(0)
}
