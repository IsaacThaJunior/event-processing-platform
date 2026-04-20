package repository

import (
	"context"
	"time"

	"github.com/isaacthajunior/mid-prod/internal/database"
	"github.com/jackc/pgx/v5/pgtype"
)

type EventRepository interface {
	SaveProcessedEvent(ctx context.Context, id, eventType, payload, status, traceID, priority, parent_id, root_id string) error
	GetEventByID(ctx context.Context, id string) (database.Event, error)
	ListProcessedEvents(ctx context.Context) ([]database.Event, error)
	LogDeliveryStatus(ctx context.Context, id, status string, attempt int, errMsg string) error
	UpdateEventStatus(ctx context.Context, id, status string) error
}

type SQLCEventRepository struct {
	q *database.Queries
}

func NewEventRepository(q *database.Queries) *SQLCEventRepository {
	return &SQLCEventRepository{q: q}
}

func (r *SQLCEventRepository) SaveProcessedEvent(ctx context.Context, id, eventType, payload, status, traceID, priority, parent_id, root_id string) error {

	return r.q.InsertEvent(ctx, database.InsertEventParams{
		ID:        id,
		Status:    pgtype.Text{String: "pending", Valid: true},
		Payload:   payload,
		CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		UpdatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		Type:      eventType,
		TraceID:   traceID,
		Priority:  pgtype.Text{String: priority, Valid: priority != ""},
		Parentid:  pgtype.Text{String: parent_id, Valid: parent_id != ""},
		Rootid:    pgtype.Text{String: root_id, Valid: root_id != ""},
	})
}

// GetEventByID fetches a processed event by ID
func (r *SQLCEventRepository) GetEventByID(ctx context.Context, id string) (database.Event, error) {
	return r.q.GetEventByID(ctx, id)
}

// ListProcessedEvents fetches all processed events
func (r *SQLCEventRepository) ListProcessedEvents(ctx context.Context) ([]database.Event, error) {
	return r.q.ListEvents(ctx)
}

func (r *SQLCEventRepository) LogDeliveryStatus(ctx context.Context, id, status string, attempt int, errMsg string) error {
	return r.q.UpsertDeliveryLog(ctx, database.UpsertDeliveryLogParams{
		EventID:      id,
		Status:       status,
		Attempt:      int32(attempt),
		ErrorMessage: pgtype.Text{String: errMsg, Valid: errMsg != ""},
	})
}

func (r *SQLCEventRepository) UpdateEventStatus(ctx context.Context, id, status string) error {
	return r.q.UpdateEventStatus(ctx, database.UpdateEventStatusParams{
		ID:     id,
		Status: pgtype.Text{String: status, Valid: true},
	})
}
