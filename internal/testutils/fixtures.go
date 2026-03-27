package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/isaacthajunior/mid-prod/internal/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CreateTestEvent creates a test event in the database using sqlc with pgxpool
func CreateTestEvent(t *testing.T, pool *pgxpool.Pool, eventID, whatsappMsgID, fromNumber, command string) string {
	t.Helper()

	if eventID == "" {
		eventID = uuid.New().String()
	}

	queries := database.New(pool)

	err := queries.InsertEvent(context.Background(), database.InsertEventParams{
		ID:                eventID,
		WhatsappMessageID: pgtype.Text{String: whatsappMsgID, Valid: whatsappMsgID != ""},
		FromNumber:        pgtype.Text{String: fromNumber, Valid: fromNumber != ""},
		Command:           pgtype.Text{String: command, Valid: command != ""},
		Payload:           `{"test": true}`,
		Status:            pgtype.Text{String: "pending", Valid: true},
		CreatedAt:         pgtype.Timestamp{Time: time.Now(), Valid: true},
		UpdatedAt:         pgtype.Timestamp{Time: time.Now(), Valid: true},
	})
	if err != nil {
		t.Fatalf("Failed to create test event: %v", err)
	}

	return eventID
}

// CleanupTable truncates a table (useful for between tests)
func CleanupTable(t *testing.T, pool *pgxpool.Pool, tableName string) {
	t.Helper()

	_, err := pool.Exec(context.Background(), "TRUNCATE TABLE "+tableName+" CASCADE")
	if err != nil {
		t.Fatalf("Failed to cleanup table %s: %v", tableName, err)
	}
}

// InsertIdempotencyKeyDirectly bypasses the service for testing
func InsertIdempotencyKeyDirectly(t *testing.T, pool *pgxpool.Pool, key, eventID string, expiresAt time.Time) {
	t.Helper()

	_, err := pool.Exec(context.Background(),
		"INSERT INTO idempotency_keys (key, event_id, expires_at, metadata) VALUES ($1, $2, $3, $4)",
		key, eventID, expiresAt, "{}",
	)
	if err != nil {
		t.Fatalf("Failed to insert idempotency key: %v", err)
	}
}

// RunInTransaction runs a function within a transaction that is automatically rolled back
func RunInTransaction(t *testing.T, pool *pgxpool.Pool, fn func(tx pgx.Tx)) {
	t.Helper()

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	fn(tx)
}
