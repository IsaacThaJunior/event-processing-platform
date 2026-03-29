// internal/handler/whatsapp_webhook.go
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"log/slog"

	"github.com/google/uuid"
	"github.com/isaacthajunior/mid-prod/internal/domain"
	"github.com/isaacthajunior/mid-prod/internal/repository"
	"github.com/isaacthajunior/mid-prod/internal/service"
)

type WhatsAppHandler struct {
	queue         domain.Queue
	commandParser *service.CommandParser
	idempotency   *service.IdempotencyRepo
	logger        *slog.Logger
	eventRepo     repository.EventRepository
	// Add your WhatsApp client here when ready
	// whatsappClient *whatsapp.Client
}

func NewWhatsAppHandler(
	queue domain.Queue,
	commandParser *service.CommandParser,
	logger *slog.Logger,
	eventRepo repository.EventRepository,
	idempotency *service.IdempotencyRepo,
) *WhatsAppHandler {
	return &WhatsAppHandler{
		queue:         queue,
		commandParser: commandParser,
		logger:        logger,
		eventRepo:     eventRepo,
		idempotency:   idempotency,
	}
}

// HandleWebhook receives messages from WhatsApp
func (h *WhatsAppHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	var payload domain.WhatsAppWebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.logger.Error("Failed to decode webhook", "error", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			for _, message := range change.Value.Messages {
				if err := h.processMessage(r.Context(), message); err != nil {
					h.logger.Error("Failed to process message",
						"message_id", message.ID,
						"error", err,
					)
				}
			}
		}
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *WhatsAppHandler) processMessage(ctx context.Context, message struct {
	From string `json:"from"`
	ID   string `json:"id"`
	Text struct {
		Body string `json:"body"`
	} `json:"text"`
	Timestamp string `json:"timestamp"`
}) error {
	// Step 1: Check idempotency - has this message been processed before?
	processed, existingEventID, err := h.idempotency.Isprocessed(ctx, message.ID)
	if err != nil {
		return err
	}

	if processed {
		h.logger.Info("Duplicate message detected, skipping",
			"message_id", message.ID,
			"existing_event_id", existingEventID,
		)
		return nil
	}

	// Step 2: Parse the command
	parseResult := h.commandParser.ParseCommand(message.Text.Body)

	h.logger.Info("Parsed command",
		"original", message.Text.Body,
		"command", parseResult.Command,
		"payload", parseResult.Payload,
	)

	// Step 3: Generate event ID
	eventID := uuid.New().String()

	// Step 4: Create event in database with full metadata
	err = h.eventRepo.SaveProcessedEvent(ctx, eventID, parseResult.Command, parseResult.Payload, message.ID, message.From, "pending")

	if err != nil {
		return err
	}

	h.logger.Info("Event created in database",
		"event_id", eventID,
		"whatsapp_message_id", message.ID,
	)

	// Step 5: Create idempotency key to prevent future duplicates
	metadata := &service.IdempotencyMetadata{
		FromNumber: message.From,
		Command:    parseResult.Command,
		Source:     "whatsapp",
		Timestamp:  time.Now().Unix(),
		Custom: map[string]interface{}{
			"whatsapp_message_id": message.ID,
			"original_text":       message.Text.Body,
		},
	}

	_, err = h.idempotency.CheckAndRecord(ctx, message.ID, eventID, metadata)
	if err != nil {
		h.logger.Error("Failed to create idempotency key", "error", err)
		// Don't fail - we already have the event
	} else {
		h.logger.Info("Idempotency key created", "key", message.ID)
	}

	// Step 6: Enqueue the event ID to Redis
	err = h.queue.Enqueue(eventID)
	if err != nil {
		// Update event status to failed if enqueue fails
		h.eventRepo.UpdateEventStatus(ctx, eventID, "failed")
		return err
	}

	h.logger.Info("Event enqueued to Redis",
		"event_id", eventID,
		"command", parseResult.Command,
		"from", message.From,
	)

	return nil
}
