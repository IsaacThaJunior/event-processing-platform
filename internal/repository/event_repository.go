package repository

import (
	"context"
	"errors"
	"time"

	"github.com/isaacthajunior/mid-prod/internal/database"
	"github.com/jackc/pgx/v5/pgtype"
	// "github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotCancellable = errors.New("task cannot be cancelled: not found or not in pending state")

type EventRepository interface {
	SaveProcessedEvent(ctx context.Context, id, eventType, payload, status, traceID, priority, parent_id string) error
	GetEventByID(ctx context.Context, id string) (database.Event, error)
	ListProcessedEvents(ctx context.Context) ([]database.Event, error)
	LogDeliveryStatus(ctx context.Context, id, status string, attempt int, errMsg string) error
	UpdateEventStatus(ctx context.Context, id, status string) error
	CancelTask(ctx context.Context, id string) error
}

type SQLCEventRepository struct {
	q *database.Queries
}

func NewEventRepository(q *database.Queries) *SQLCEventRepository {
	return &SQLCEventRepository{q: q}
}

func (r *SQLCEventRepository) SaveProcessedEvent(ctx context.Context, id, eventType, payload, status, traceID, priority, parent_id string) error {

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

func (r *SQLCEventRepository) CancelTask(ctx context.Context, id string) error {
	tag, err := r.q.CancelEventIfPending(ctx, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotCancellable
	}
	return nil
}
