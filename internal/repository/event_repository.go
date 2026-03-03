package repository

import (
	"context"
	"time"

	"github.com/isaacthajunior/mid-prod/internal/database"
	"github.com/jackc/pgx/v5/pgtype"
)

type EventRepository interface {
	SaveProcessedEvent(ctx context.Context, id, eventType, payload string) error
	GetEventByID(ctx context.Context, id string) (database.Event, error)
	ListProcessedEvents(ctx context.Context) ([]database.Event, error)
}

type SQLCEventRepository struct {
	q *database.Queries
}

func NewEventRepository(q *database.Queries) *SQLCEventRepository {
	return &SQLCEventRepository{q: q}
}

func (r *SQLCEventRepository) SaveProcessedEvent(ctx context.Context, id, eventType, payload string) error {
	return r.q.InsertEvent(ctx, database.InsertEventParams{
		ID:        id,
		Type:      eventType,
		Payload:   payload,
		CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		Processed: pgtype.Bool{Bool: true},
	})
}

// GetEventByID fetches a processed event by ID
func (r *SQLCEventRepository) GetEventByID(ctx context.Context, id string) (database.Event, error) {
	return r.q.GetEventByID(ctx, id)
}

// ListProcessedEvents fetches all processed events
func (r *SQLCEventRepository) ListProcessedEvents(ctx context.Context) ([]database.Event, error) {
	return r.q.ListProcessedEvents(ctx)
}
