package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/isaacthajunior/mid-prod/internal/database"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type IdempotencyRepo struct {
	q  *database.Queries
	db *pgxpool.Pool
}

// IdempotencyRecord represents a stored idempotency record
type IdempotencyRecord struct {
	Key       string          `json:"key"`
	EventID   string          `json:"event_id"`
	CreatedAt time.Time       `json:"created_at"`
	ExpiresAt time.Time       `json:"expires_at"`
	Metadata  json.RawMessage `json:"metadata"`
}

// IdempotencyMetadata contains additional context for the idempotency record
type IdempotencyMetadata struct {
	FromNumber string                 `json:"from_number,omitempty"`
	Command    string                 `json:"command,omitempty"`
	Source     string                 `json:"source,omitempty"` // whatsapp, api, etc.
	Timestamp  int64                  `json:"timestamp,omitempty"`
	Custom     map[string]any `json:"custom,omitempty"`
}

func NewIdempotencyService(q *database.Queries, db *pgxpool.Pool) *IdempotencyRepo {
	return &IdempotencyRepo{
		q:  q,
		db: db,
	}
}

func (s *IdempotencyRepo) CheckAndRecord(
	ctx context.Context,
	key,
	eventID string,
	metadata *IdempotencyMetadata,
) (bool, error) {
	// Use a transaction for this
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("Failed to start transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	// Create transaction-specific queries
	qtx := s.q.WithTx(tx)

	_, err = qtx.GetIdempotencyKey(ctx, key)
	// This is the happy path
	if err == nil {
		return true, nil
	}
	if err != sql.ErrNoRows {
		return false, fmt.Errorf("failed to check idempotency: %w", err)
	}

	// Prepare metadata JSON
	metadataJSON := "{}"
	if metadata != nil {
		jsonBytes, err := json.Marshal(metadata)
		if err != nil {
			return false, fmt.Errorf("failed to marshal metadata: %w", err)
		}
		metadataJSON = string(jsonBytes)
	}

	// Key doesn't exist - insert it
	expiresAt := time.Now().Add(30 * 24 * time.Hour)

	err = qtx.CreateIdempotencyKey(ctx, database.CreateIdempotencyKeyParams{
		Key:       key,
		EventID:   eventID,
		ExpiresAt: pgtype.Timestamp{Time: expiresAt, Valid: true},
		Metadata:  []byte(metadataJSON),
	})
	if err != nil {
		return false, fmt.Errorf("failed to insert idempotency key: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return false, nil
}

func (s *IdempotencyRepo) Isprocessed(ctx context.Context, key string) (bool, string, error) {
	idempotencyKey, err := s.q.GetIdempotencyKey(ctx, key)
	if err == sql.ErrNoRows {
		return false, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("failed to check idempotency: %w", err)
	}

	return true, idempotencyKey.EventID, nil
}

// GetRecord retrieves the full idempotency record for a key
func (s *IdempotencyRepo) GetRecord(ctx context.Context, key string) (*IdempotencyRecord, error) {
	idempotencyKey, err := s.q.GetIdempotencyKey(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get idempotency record: %w", err)
	}

	return &IdempotencyRecord{
		Key:       idempotencyKey.Key,
		EventID:   idempotencyKey.EventID,
		CreatedAt: idempotencyKey.CreatedAt.Time,
		ExpiresAt: idempotencyKey.ExpiresAt.Time,
		Metadata:  []byte(idempotencyKey.Metadata),
	}, nil
}

// CleanupExpired removes expired idempotency keys
func (s *IdempotencyRepo) CleanupExpired(ctx context.Context) (int64, error) {
	rows, err := s.q.DeleteExpiredIdempotencyKeys(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup expired keys: %w", err)
	}
	return rows, nil
}

// GetStats returns statistics about idempotency keys
func (s *IdempotencyRepo) GetStats(ctx context.Context) (total, active, expired int64, err error) {
	stats, err := s.q.GetIdempotencyKeyStats(ctx)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to get stats: %w", err)
	}

	return stats.TotalKeys, stats.ActiveKeys, stats.ExpiredKeys, nil
}

// WithIdempotency is a helper that wraps a function with idempotency check and might be used for handling Whatsapp webhooks
func (s *IdempotencyRepo) WithIdempotency(
	ctx context.Context,
	key string,
	metadata *IdempotencyMetadata,
	handler func(ctx context.Context, eventID string) error,
) (bool, string, error) {
	// Generate event ID if needed
	eventID := uuid.New().String()

	// Check and record idempotency
	processed, err := s.CheckAndRecord(ctx, key, eventID, metadata)
	if err != nil {
		return false, "", fmt.Errorf("idempotency check failed: %w", err)
	}

	if processed {
		// Already processed, get the existing event ID
		_, existingEventID, err := s.Isprocessed(ctx, key)
		if err != nil {
			return true, "", err
		}
		return true, existingEventID, nil
	}

	return false, eventID, nil
}
