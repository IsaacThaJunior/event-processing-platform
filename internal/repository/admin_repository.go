package repository

import (
	"context"
	"errors"

	"github.com/isaacthajunior/mid-prod/internal/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var ErrNotRetryable = errors.New("task cannot be retried: not found or not in a failed/cancelled state")

type AdminRepository struct {
	q *database.Queries
}

func NewAdminRepository(q *database.Queries) *AdminRepository {
	return &AdminRepository{q: q}
}

type ListEventsParams struct {
	Status   string
	Type     string
	Priority string
	Search   string
	Page     int
	PageSize int
}

type StatusCount struct {
	Status string
	Count  int64
}

func (r *AdminRepository) ListEvents(ctx context.Context, p ListEventsParams) ([]database.Event, int64, error) {
	if p.PageSize <= 0 {
		p.PageSize = 20
	}
	if p.Page <= 0 {
		p.Page = 1
	}

	toText := func(s string) pgtype.Text {
		return pgtype.Text{String: s, Valid: s != ""}
	}

	total, err := r.q.CountEventsFiltered(ctx, database.CountEventsFilteredParams{
		Status:   toText(p.Status),
		Type:     toText(p.Type),
		Priority: toText(p.Priority),
		Search:   toText(p.Search),
	})
	if err != nil {
		return nil, 0, err
	}

	offset := int32((p.Page - 1) * p.PageSize)
	events, err := r.q.ListEventsFiltered(ctx, database.ListEventsFilteredParams{
		Status:   toText(p.Status),
		Type:     toText(p.Type),
		Priority: toText(p.Priority),
		Search:   toText(p.Search),
		Limit:    int32(p.PageSize),
		Offset:   offset,
	})
	if err != nil {
		return nil, 0, err
	}
	return events, total, nil
}

func (r *AdminRepository) GetEventByID(ctx context.Context, id string) (database.Event, error) {
	return r.q.GetEventByID(ctx, id)
}

func (r *AdminRepository) GetStatusCounts(ctx context.Context) ([]StatusCount, error) {
	rows, err := r.q.GetEventStatusCounts(ctx)
	if err != nil {
		return nil, err
	}
	counts := make([]StatusCount, len(rows))
	for i, row := range rows {
		counts[i] = StatusCount{Status: row.Status, Count: row.Count}
	}
	return counts, nil
}

func (r *AdminRepository) GetRecentProcessedCount(ctx context.Context) (int64, error) {
	return r.q.GetRecentProcessedCount(ctx)
}

func (r *AdminRepository) GetRetryHistory(ctx context.Context, taskID string) ([]database.EventDeliveryLog, error) {
	return r.q.GetDeliveryLogsForEvent(ctx, taskID)
}

func (r *AdminRepository) ResetTaskForRetry(ctx context.Context, id string) (string, error) {
	priority, err := r.q.ResetTaskForRetry(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotRetryable
	}
	if err != nil {
		return "", err
	}
	p := priority.String
	if !priority.Valid || p == "" {
		p = "medium"
	}
	return p, nil
}
