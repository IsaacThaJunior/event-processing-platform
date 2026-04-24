package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/isaacthajunior/mid-prod/internal/database"
	"github.com/jackc/pgx/v5"
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
	Command   string         `json:"command,omitempty"`
	Source    string         `json:"source,omitempty"` // whatsapp, api, etc.
}

func NewIdempotencyService(q *database.Queries, db *pgxpool.Pool) *IdempotencyRepo {
	return &IdempotencyRepo{
		q:  q,
		db: db,
	}
}

func (s *IdempotencyRepo) CheckAndRecordToDB(
	ctx context.Context,
	key,
	eventID string,
	metadata *IdempotencyMetadata,
) (bool, error) {
	slog.Info("CheckAndRecord called",
		"key", key,
		"event_id", eventID,
		"metadata", metadata)

	// Prepare metadata JSON
	metadataJSON := "{}"
	if metadata != nil {
		jsonBytes, err := json.Marshal(metadata)
		if err != nil {
			slog.Error("Failed to marshal metadata", "error", err)
			return false, fmt.Errorf("failed to marshal metadata: %w", err)
		}
		metadataJSON = string(jsonBytes)
		slog.Info("Metadata marshaled", "metadata", metadataJSON)
	}

	// Set expiration (30 days from now)
	expiresAt := time.Now().Add(30 * 24 * time.Hour)

	// Try to insert - this is atomic and handles race conditions automatically
	rowsAffected, err := s.q.CreateIdempotencyKey(ctx, database.CreateIdempotencyKeyParams{
		Key:       key,
		EventID:   eventID,
		ExpiresAt: pgtype.Timestamp{Time: expiresAt, Valid: true},
		Metadata:  []byte(metadataJSON),
	})

	if err != nil {
		slog.Error("Failed to insert idempotency key", "error", err)
		return false, fmt.Errorf("failed to insert idempotency key: %w", err)
	}

	// rowsAffected == 1 means we inserted it (first time - NOT processed before)
	// rowsAffected == 0 means it already existed (ALREADY processed before)
	alreadyProcessed := rowsAffected == 0

	if alreadyProcessed {
		slog.Info("Idempotency key already existed", "key", key, "event_id", eventID)
	} else {
		slog.Info("Idempotency key created successfully", "key", key, "event_id", eventID)
	}

	return alreadyProcessed, nil
}

// This is a read only func for checking if a key exists and nothing more
func (s *IdempotencyRepo) Isprocessed(ctx context.Context, key string) (bool, string, error) {
	idempotencyKey, err := s.q.GetIdempotencyKey(ctx, key)

	if err != nil {
		if err == pgx.ErrNoRows {
			return false, "", nil
		}
		return false, "", fmt.Errorf("failed to check idempotency: %w", err)
	}

	return true, idempotencyKey.EventID, nil
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

func (s *IdempotencyRepo) DeleteKey(ctx context.Context, idKey string) error {
	return s.q.DeleteIdempotencyKey(ctx, idKey)
}

func (s *IdempotencyRepo) GenerateIdempotencyKey(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte(":"))
	}
	return "key_" + hex.EncodeToString(h.Sum(nil))[:32]
}
