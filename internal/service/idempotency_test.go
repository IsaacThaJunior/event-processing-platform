package service

import (
	"context"
	"testing"
	"time"

	"github.com/isaacthajunior/mid-prod/internal/database"
	testutil "github.com/isaacthajunior/mid-prod/internal/testutils"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdempotencyService(t *testing.T) {
	pool, cleanup := testutil.SetupTestDB(t)
	defer cleanup()
	queries := database.New(pool)

	idempotency := NewIdempotencyService(queries, pool)
	ctx := context.Background()

	t.Run("CheckAndRecordToDB - first time", func(t *testing.T) {
		key := "test-key-001"
		eventID := "event-001"

		err := queries.InsertEvent(ctx, database.InsertEventParams{
			ID:        eventID,
			Payload:   `{"test": true}`,
			CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
			UpdatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		})
		require.NoError(t, err)

		processed, err := idempotency.CheckAndRecordToDB(ctx, key, eventID, nil)
		require.NoError(t, err)
		assert.False(t, processed, "First time should not be processed")

		exists, _, err := idempotency.Isprocessed(ctx, key)
		require.NoError(t, err)
		assert.True(t, exists, "Key should exist after recording")
	})

	t.Run("CheckAndRecordToDB - duplicate", func(t *testing.T) {
		key := "test-key-002"
		eventID := "event-002"

		err := queries.InsertEvent(ctx, database.InsertEventParams{
			ID:        eventID,
			Payload:   `{"test": true}`,
			CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
			UpdatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		})
		require.NoError(t, err)

		processed, err := idempotency.CheckAndRecordToDB(ctx, key, eventID, nil)
		require.NoError(t, err)
		assert.False(t, processed)

		processed, err = idempotency.CheckAndRecordToDB(ctx, key, eventID, nil)
		require.NoError(t, err)
		assert.True(t, processed, "Duplicate should be marked as processed")
	})

	t.Run("CheckAndRecordToDB with metadata", func(t *testing.T) {
		key := "test-key-metadata"
		eventID := "event-metadata"

		err := queries.InsertEvent(ctx, database.InsertEventParams{
			ID:        eventID,
			Payload:   `{"test": true}`,
			CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
			UpdatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		})
		require.NoError(t, err)

		metadata := &IdempotencyMetadata{
			Command:   "resize_image",
			Source:    "whatsapp",
		}

		processed, err := idempotency.CheckAndRecordToDB(ctx, key, eventID, metadata)
		require.NoError(t, err)
		assert.False(t, processed)

	})

	t.Run("IsProcessed", func(t *testing.T) {
		key := "test-key-003"
		eventID := "event-003"

		err := queries.InsertEvent(ctx, database.InsertEventParams{
			ID:        eventID,
			Payload:   `{"test": true}`,
			CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
			UpdatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		})
		require.NoError(t, err)

		processed, foundEventID, err := idempotency.Isprocessed(ctx, key)
		require.NoError(t, err)
		assert.False(t, processed)
		assert.Equal(t, "", foundEventID)

		_, err = idempotency.CheckAndRecordToDB(ctx, key, eventID, nil)
		require.NoError(t, err)

		processed, foundEventID, err = idempotency.Isprocessed(ctx, key)
		require.NoError(t, err)
		assert.True(t, processed)
		assert.Equal(t, eventID, foundEventID)
	})

	t.Run("GetRecord", func(t *testing.T) {
		key := "test-key-record"
		eventID := "event-record"

		err := queries.InsertEvent(ctx, database.InsertEventParams{
			ID:        eventID,
			Payload:   `{"test": true}`,
			CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
			UpdatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		})
		require.NoError(t, err)

		_, err = idempotency.CheckAndRecordToDB(ctx, key, eventID, nil)
		require.NoError(t, err)




	})

	t.Run("CleanupExpired", func(t *testing.T) {
		key := "expired-key"
		eventID := "expired-event"

		err := queries.InsertEvent(ctx, database.InsertEventParams{
			ID:        eventID,
			Payload:   `{"test": true}`,
			CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
			UpdatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		})
		require.NoError(t, err)

		// Insert expired key directly using the testutil helper

		rows, err := idempotency.CleanupExpired(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), rows)

		processed, _, err := idempotency.Isprocessed(ctx, key)
		require.NoError(t, err)
		assert.False(t, processed)
	})

	t.Run("GetStats", func(t *testing.T) {
		// Create some test keys
		for i := 0; i < 3; i++ {
			key := "stats-key"
			eventID := "stats-event"

			err := queries.InsertEvent(ctx, database.InsertEventParams{
				ID:        eventID,
				Payload:   `{"test": true}`,
				CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
				UpdatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
			})
			require.NoError(t, err)

			_, err = idempotency.CheckAndRecordToDB(ctx, key, eventID, nil)
			require.NoError(t, err)
		}

		total, active, expired, err := idempotency.GetStats(ctx)
		require.NoError(t, err)

		assert.GreaterOrEqual(t, total, int64(3))
		assert.GreaterOrEqual(t, active, int64(3))
		assert.GreaterOrEqual(t, expired, int64(0))
	})

	t.Run("Concurrent check and record", func(t *testing.T) {
		key := "concurrent-key"
		eventID := "concurrent-event"

		err := queries.InsertEvent(ctx, database.InsertEventParams{
			ID:        eventID,
			Payload:   `{"test": true}`,
			CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
			UpdatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		})
		require.NoError(t, err)

		const numGoroutines = 20
		results := make(chan bool, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func() {
				processed, err := idempotency.CheckAndRecordToDB(ctx, key, eventID, nil)
				assert.NoError(t, err)
				results <- processed
			}()
		}

		processedCount := 0
		notProcessedCount := 0

		for i := 0; i < numGoroutines; i++ {
			if <-results {
				processedCount++
			} else {
				notProcessedCount++
			}
		}

		assert.Equal(t, 1, notProcessedCount, "Only one should be first")
		assert.Equal(t, numGoroutines-1, processedCount, "Rest should be duplicates")
	})

}
