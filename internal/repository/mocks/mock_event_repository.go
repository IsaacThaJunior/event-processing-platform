// internal/repository/mocks/mock_event_repository.go
package mocks

import (
	"context"

	"github.com/isaacthajunior/mid-prod/internal/database"
	"github.com/stretchr/testify/mock"
)

type MockEventRepository struct {
	mock.Mock
}

// SaveProcessedEvent is called by the worker
func (m *MockEventRepository) SaveProcessedEvent(ctx context.Context, id, eventType, payload string) error {
	args := m.Called(ctx, id, eventType, payload)
	return args.Error(0)
}

// LogDeliveryStatus is called by the worker
func (m *MockEventRepository) LogDeliveryStatus(ctx context.Context, id, status string, attempt int, errMsg string) error {
	args := m.Called(ctx, id, status, attempt, errMsg)
	return args.Error(0)
}

// GetEventByID - not used by worker, but part of interface
func (m *MockEventRepository) GetEventByID(ctx context.Context, id string) (database.Event, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return database.Event{}, args.Error(1)
	}
	return args.Get(0).(database.Event), args.Error(1)
}

// ListProcessedEvents - not used by worker, but part of interface
func (m *MockEventRepository) ListProcessedEvents(ctx context.Context) ([]database.Event, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]database.Event), args.Error(1)
}
