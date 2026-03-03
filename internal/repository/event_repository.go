package repository

import (
	"context"
	"time"

	"github.com/isaacthajunior/mid-prod/internal/database"
	"github.com/jackc/pgx/v5/pgtype"
)

type EventRepository interface {
	SaveProcessedEvent(ctx context.Context, id string) error
}

type SQLCEventRepository struct {
	q *database.Queries
}

func NewEventRepository(q *database.Queries) *SQLCEventRepository {
	return &SQLCEventRepository{q: q}
}

func (r *SQLCEventRepository) SaveProcessedEvent(ctx context.Context, id string) error {
	return r.q.InsertEvent(ctx, database.InsertEventParams{
		ID:        id,
		Type:      "processed",
		Payload:   "",
		CreatedAt: pgtype.Timestamp{Time: time.Now()},
		Processed: pgtype.Bool{Bool: true},
	})
}
