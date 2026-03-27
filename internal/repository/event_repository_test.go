package repository

import (
	"context"
	"testing"
	"time"

	"github.com/isaacthajunior/mid-prod/internal/database"
	testutil "github.com/isaacthajunior/mid-prod/internal/testutils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventRepositoryWithSqlc(t *testing.T) {
	pool, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	queries := database.New(pool)
	ctx := context.Background()

	t.Run("Create and Get Event", func(t *testing.T) {
		eventID := "test-event-1"
		whatsappMsgID := "whatsapp-123"

		err := queries.InsertEvent(ctx, database.InsertEventParams{
			ID:                eventID,
			WhatsappMessageID: pgtype.Text{String: whatsappMsgID, Valid: true},
			FromNumber:        pgtype.Text{String: "+1234567890", Valid: true},
			Command:           pgtype.Text{String: "resize_image", Valid: true},
			Payload:           `{"image":"test.jpg"}`,
			Status:            pgtype.Text{String: "pending", Valid: true},
			CreatedAt:         pgtype.Timestamp{Time: time.Now(), Valid: true},
			UpdatedAt:         pgtype.Timestamp{Time: time.Now(), Valid: true},
		})
		require.NoError(t, err)

		event, err := queries.GetEventByID(ctx, eventID)
		require.NoError(t, err)

		assert.Equal(t, eventID, event.ID)
		assert.Equal(t, whatsappMsgID, event.WhatsappMessageID)
		assert.Equal(t, "resize_image", event.Command)
	})

	t.Run("Get non-existent event", func(t *testing.T) {
		_, err := queries.GetEventByWhatsappMessageID(ctx, pgtype.Text{String: "non-existent", Valid: true})
		assert.Error(t, err)
		// pgx returns pgx.ErrNoRows for not found
		assert.ErrorIs(t, err, pgx.ErrNoRows)
	})

	t.Run("Update Event Status", func(t *testing.T) {
		eventID := "test-event-2"

		err := queries.InsertEvent(ctx, database.InsertEventParams{
			ID:        eventID,
			Payload:   `{}`,
			CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
			UpdatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		})
		require.NoError(t, err)

		err = queries.UpdateEventStatus(ctx, database.UpdateEventStatusParams{
			ID:     eventID,
			Status: pgtype.Text{String: "processed", Valid: true},
		})
		require.NoError(t, err)

		event, err := queries.GetEventByID(ctx, eventID)
		require.NoError(t, err)
		assert.Equal(t, "processing", event.Status)
	})

	t.Run("GetEventByWhatsappMessageID", func(t *testing.T) {
		eventID := "test-event-3"
		whatsappMsgID := "whatsapp-unique-123"

		err := queries.InsertEvent(ctx, database.InsertEventParams{
			ID:                eventID,
			WhatsappMessageID: pgtype.Text{String: whatsappMsgID, Valid: true},
			Payload:           `{}`,
			CreatedAt:         pgtype.Timestamp{Time: time.Now(), Valid: true},
			UpdatedAt:         pgtype.Timestamp{Time: time.Now(), Valid: true},
		})
		require.NoError(t, err)

		event, err := queries.GetEventByWhatsappMessageID(ctx, pgtype.Text{String: whatsappMsgID, Valid: true})
		require.NoError(t, err)
		assert.Equal(t, eventID, event.ID)
	})

	t.Run("CreateDeliveryLog", func(t *testing.T) {
		eventID := "test-event-4"

		err := queries.InsertEvent(ctx, database.InsertEventParams{
			ID:        eventID,
			Payload:   `{}`,
			CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
			UpdatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		})
		require.NoError(t, err)

		err = queries.InsertDeliveryLog(ctx, database.InsertDeliveryLogParams{
			EventID:      eventID,
			Status:       "processed",
			Attempt:      1,
			ErrorMessage: pgtype.Text{String: "", Valid: true}, // Empty string for no error
		})
		require.NoError(t, err)

		logs, err := queries.GetDeliveryLogsForEvent(ctx, eventID)
		require.NoError(t, err)
		assert.Len(t, logs, 1)
		assert.Equal(t, "processed", logs[0].Status)
		assert.Equal(t, int32(1), logs[0].Attempt)
	})

	t.Run("UpdateEventProcessed", func(t *testing.T) {
		eventID := "test-event-5"

		err := queries.InsertEvent(ctx, database.InsertEventParams{
			ID:        eventID,
			Payload:   `{}`,
			Status:    pgtype.Text{String: "pending", Valid: true},
			CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
			UpdatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		})
		require.NoError(t, err)

		err = queries.UpdateEventStatus(ctx, database.UpdateEventStatusParams{
			ID:     eventID,
			Status: pgtype.Text{String: "processed", Valid: true},
		})
		require.NoError(t, err)

		event, err := queries.GetEventByID(ctx, eventID)
		require.NoError(t, err)
		assert.Equal(t, "processed", event.Status)
	})
}

// Test with transaction isolation
func TestEventRepositoryWithTransaction(t *testing.T) {
	pool, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("Transaction rollback on error", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx)

		qtx := database.New(tx)

		eventID := "tx-test-event"
		err = qtx.InsertEvent(ctx, database.InsertEventParams{
			ID:        eventID,
			Payload:   `{"test": true}`,
			CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
			UpdatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		})
		require.NoError(t, err)

		// Event exists in transaction
		_, err = qtx.GetEventByID(ctx, eventID)
		require.NoError(t, err)

		// Rollback
		err = tx.Rollback(ctx)
		require.NoError(t, err)

		// Event should not exist after rollback
		_, err = database.New(pool).GetEventByID(ctx, eventID)
		assert.ErrorIs(t, err, pgx.ErrNoRows)
	})
}
