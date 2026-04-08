package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/isaacthajunior/mid-prod/internal/domain"
	"github.com/isaacthajunior/mid-prod/internal/middleware"
	"github.com/isaacthajunior/mid-prod/internal/repository"
	"github.com/isaacthajunior/mid-prod/internal/sender"
	"github.com/isaacthajunior/mid-prod/internal/service"
)

type TaskHandler struct {
	queue       domain.Queue
	eventRepo   repository.EventRepository
	idempotency *service.IdempotencyRepo
	logger      *slog.Logger
}

type TaskRequest struct {
	Type     string          `json:"type"`
	Payload  json.RawMessage `json:"payload"`
	Priority string          `json:"priority"`
}

func NewTaskHanler(queue domain.Queue, eventRepo repository.EventRepository, id *service.IdempotencyRepo, logger *slog.Logger) *TaskHandler {
	return &TaskHandler{
		queue:       queue,
		eventRepo:   eventRepo,
		idempotency: id,
		logger:      logger,
	}
}

func (h *TaskHandler) HandleCreateTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	traceID := r.Context().Value(middleware.TraceIDKey).(string)

	var req TaskRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sender.RespondWithError(w, http.StatusBadRequest, "Invalid body", err)
		return
	}

	if req.Type == "" {
		sender.RespondWithError(w, http.StatusBadRequest, "Type Required", errors.New("Type Required"))
		return
	}

	h.logger.Info("creating task",
		"trace_id", traceID,
		"type", req.Type,
	)

	key := h.idempotency.GenerateIdempotencyKey(req.Type, string(req.Payload), req.Priority)

	processed, existingEventID, err := h.idempotency.Isprocessed(ctx, key)
	if err != nil {
		sender.RespondWithError(w, http.StatusInternalServerError, "idempotency check failed", err)
		return
	}

	if processed {
		sender.RespondWithJSON(w, http.StatusConflict, map[string]any{
			"status":   "duplicate",
			"event_id": existingEventID,
		})
		return
	}

	eventID := uuid.New().String()

	// Save event
	err = h.eventRepo.SaveProcessedEvent(ctx, eventID, req.Type, string(req.Payload), "pending", traceID, req.Priority)
	if err != nil {
		sender.RespondWithError(w, http.StatusInternalServerError, "Failed to save event", err)
		return
	}

	// Record idempotency
	meta := &service.IdempotencyMetadata{
		Command:   req.Type,
		Source:    "api",
		Timestamp: time.Now().Unix(),
	}
	_, err = h.idempotency.CheckAndRecord(ctx, key, eventID, meta)
	if err != nil {
		sender.RespondWithError(w, http.StatusInternalServerError, "idempotency check and recording failed", err)
		return
	}

	// Enqueue
	if err := h.queue.Enqueue(eventID); err != nil {
		h.eventRepo.UpdateEventStatus(ctx, eventID, "failed")
		sender.RespondWithError(w, http.StatusInternalServerError, "Failed to enqueue to Redis", err)
		return
	}

	// Send Success
	sender.RespondWithJSON(w, http.StatusOK, map[string]any{
		"status":   "accepted",
		"event_id": eventID,
	})

}
