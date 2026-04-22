package mocks

import (
	"context"

	"github.com/isaacthajunior/mid-prod/internal/database"
	"github.com/stretchr/testify/mock"
)

type MockEventRepository struct {
	mock.Mock
}

func (m *MockEventRepository) SaveProcessedEvent(ctx context.Context, id, eventType, payload, status, traceID, priority, parentID string) error {
	args := m.Called(ctx, id, eventType, payload, status, traceID, priority, parentID)
	return args.Error(0)
}

func (m *MockEventRepository) GetEventByID(ctx context.Context, id string) (database.Event, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return database.Event{}, args.Error(1)
	}
	return args.Get(0).(database.Event), args.Error(1)
}

func (m *MockEventRepository) ListProcessedEvents(ctx context.Context) ([]database.Event, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]database.Event), args.Error(1)
}

func (m *MockEventRepository) LogDeliveryStatus(ctx context.Context, id, status string, attempt int, errMsg string) error {
	args := m.Called(ctx, id, status, attempt, errMsg)
	return args.Error(0)
}

func (m *MockEventRepository) UpdateEventStatus(ctx context.Context, id, status string) error {
	args := m.Called(ctx, id, status)
	return args.Error(0)
}

func (m *MockEventRepository) CancelTask(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}
