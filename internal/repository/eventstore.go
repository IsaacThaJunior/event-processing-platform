package repository

import (
	"context"
	"encoding/json"

	"github.com/isaacthajunior/pulse/worker"
)

// EventStore adapts an EventRepository (Postgres-shaped, wire-format aware)
// to the generic worker.Store interface the Pool depends on.
type EventStore struct {
	repo EventRepository
}

func NewEventStore(repo EventRepository) *EventStore {
	return &EventStore{repo: repo}
}

var _ worker.Store = (*EventStore)(nil)

// requestWrapper mirrors the JSON shape task_handler.go persists
// (handler.TaskRequest) closely enough to pull out the inner payload and
// trace context. Defined here instead of importing internal/handler to
// avoid an import cycle (handler already imports repository).
type requestWrapper struct {
	Payload      json.RawMessage `json:"payload"`
	TraceContext string          `json:"trace_context"`
	Next         json.RawMessage `json:"next,omitempty"`
}

func (s *EventStore) GetTask(ctx context.Context, id string) (worker.Task, error) {
	event, err := s.repo.GetEventByID(ctx, id)
	if err != nil {
		return worker.Task{}, err
	}

	var wrapper requestWrapper
	payload := json.RawMessage(event.Payload)
	if err := json.Unmarshal([]byte(event.Payload), &wrapper); err == nil && len(wrapper.Payload) > 0 {
		payload = wrapper.Payload
	}

	meta := make(map[string]string)
	if wrapper.TraceContext != "" {
		meta["trace_context"] = wrapper.TraceContext
	}
	if event.TraceID != "" {
		meta["trace_id"] = event.TraceID
	}
	if len(wrapper.Next) > 0 {
		meta["next"] = string(wrapper.Next)
	}
	rootTaskID := event.ID
	if event.Parentid.Valid && event.Parentid.String != "" {
		rootTaskID = event.Parentid.String
	}
	meta["root_task_id"] = rootTaskID

	priority := ""
	if event.Priority.Valid {
		priority = event.Priority.String
	}
	status := ""
	if event.Status.Valid {
		status = event.Status.String
	}

	return worker.Task{
		ID:       event.ID,
		Type:     event.Type,
		Payload:  payload,
		Priority: priority,
		Status:   status,
		Metadata: meta,
	}, nil
}

func (s *EventStore) UpdateStatus(ctx context.Context, id, status string) error {
	return s.repo.UpdateEventStatus(ctx, id, status)
}

func (s *EventStore) RecordAttempt(ctx context.Context, id, status string, attempt int, errMsg string) error {
	return s.repo.LogDeliveryStatus(ctx, id, status, attempt, errMsg)
}
