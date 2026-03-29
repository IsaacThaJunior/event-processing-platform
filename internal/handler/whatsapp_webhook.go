// internal/handler/whatsapp_webhook.go
package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
		h.logger.Error("Idempotency check failed", "error", err, "message_id", message.ID)
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

// HandleWebhook receives messages from WhatsApp
func (h *WhatsAppHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	// For GET requests - verification
	if r.Method == http.MethodGet {
		h.HandleVerification(w, r)
		return
	}

	// For POST requests - actual webhook
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload domain.WhatsAppWebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.logger.Error("Failed to decode webhook", "error", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Process each message
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

// HandleVerification handles WhatsApp webhook verification
func (h *WhatsAppHandler) HandleVerification(w http.ResponseWriter, r *http.Request) {
	// WhatsApp sends a GET request with hub.challenge for verification
	challenge := r.URL.Query().Get("hub.challenge")
	if challenge != "" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(challenge))
		return
	}

	// If no challenge, it's a test request
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "verification endpoint ready"})
}

func generateIdempotencyKey(command, fromNumber, payload string) string {
	content := fmt.Sprintf("%s:%s:%s", command, fromNumber, payload)
	// Create SHA256 hash
	hash := sha256.Sum256([]byte(content))
	// Return first 32 characters of hex hash (enough for uniqueness)
	return "msg_" + hex.EncodeToString(hash[:16])
}

// HandleTestPush is a test endpoint for manual testing
func (h *WhatsAppHandler) HandleTestPush(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Command      string `json:"command"`
		FromNumber   string `json:"from_number"`
		Payload      string `json:"payload"`
		OriginalText string `json:"original_text"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Generate DETERMINISTIC idempotency key based on content
	// Same content = same key EVERY time
	idempotencyKey := generateIdempotencyKey(req.Command, req.FromNumber, req.Payload)

	whatsappMsgID := idempotencyKey // Use the deterministic key as the whatsapp_message_id

	h.logger.Info("Processing test push",
		"idempotency_key", idempotencyKey,
		"command", req.Command,
		"from", req.FromNumber,
	)

	// STEP 1: Check idempotency FIRST
	processed, existingEventID, err := h.idempotency.Isprocessed(r.Context(), idempotencyKey)
	if err != nil {
		h.logger.Error("Idempotency check failed", "error", err)
		http.Error(w, "Idempotency check failed", http.StatusInternalServerError)
		return
	}

	if processed {
		// Duplicate detected - reject immediately
		h.logger.Info("Duplicate message rejected by idempotency",
			"idempotency_key", idempotencyKey,
			"existing_event_id", existingEventID,
		)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":            "duplicate",
			"message":           "This exact message has already been processed",
			"existing_event_id": existingEventID,
			"idempotency_key":   idempotencyKey,
		})
		return
	}

	// Step 2: Generate IDs
	eventID := uuid.New().String()

	h.logger.Info("First time request - creating new event",
		"event_id", eventID,
		"idempotency_key", idempotencyKey,
	)

	// STEP 3: Create event in database FIRST (so the event exists for the foreign key)
	err = h.eventRepo.SaveProcessedEvent(r.Context(), eventID, req.Command, req.Payload, whatsappMsgID, req.FromNumber, "pending")

	if err != nil {
		h.logger.Error("Failed to create event", "error", err)
		// Clean up the idempotency key since event creation failed
		_ = h.idempotency.DeleteKey(r.Context(), idempotencyKey)
		http.Error(w, fmt.Sprintf("Failed to create event: %v", err), http.StatusInternalServerError)
		return
	}
	h.logger.Info("Event created in database", "event_id", eventID)

	// STEP 4: Create idempotency key with event ID
	metadata := &service.IdempotencyMetadata{
		FromNumber: req.FromNumber,
		Command:    req.Command,
		Source:     "test_api",
		Timestamp:  time.Now().Unix(),
		Custom: map[string]any{
			"original_text":   req.OriginalText,
			"test_endpoint":   true,
			"idempotency_key": idempotencyKey,
		},
	}
	h.logger.Info("About to call CheckAndRecord",
		"whatsappMsgID", whatsappMsgID,
		"eventID", eventID)

	processed, err = h.idempotency.CheckAndRecord(r.Context(), idempotencyKey, eventID, metadata)
	if err != nil {
		h.logger.Error("Failed to create idempotency key", "error", err)
		http.Error(w, "Failed to create idempotency key", http.StatusInternalServerError)
		return
	}

	if processed {
		// Race condition - another request created it between our check and this call
		h.logger.Warn("Race condition: key was created by another process",
			"idempotency_key", idempotencyKey)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":          "duplicate",
			"message":         "This message was processed by another concurrent request",
			"idempotency_key": idempotencyKey,
		})
		return
	}

	// STEP 5: Push to Redis
	if err := h.queue.Enqueue(eventID); err != nil {
		h.logger.Error("Failed to enqueue to Redis", "error", err)
		http.Error(w, fmt.Sprintf("Failed to enqueue: %v", err), http.StatusInternalServerError)
		return
	}

	h.logger.Info("Test event pushed successfully",
		"event_id", eventID,
		"command", req.Command,
		"from", req.FromNumber,
		"idempotency_key", idempotencyKey,
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          "accepted",
		"event_id":        eventID,
		"idempotency_key": idempotencyKey,
		"message":         "Test event pushed successfully",
	})
}
