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
	validator   *service.TaskValidator
}

type TaskRequest struct {
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Priority  string          `json:"priority"`
	ExecuteAt *time.Time      `json:"execute_at,omitempty"`

	Next *TaskRequest `json:"next,omitempty"`
}

func NewTaskHanler(queue domain.Queue, eventRepo repository.EventRepository, id *service.IdempotencyRepo, logger *slog.Logger, validator *service.TaskValidator) *TaskHandler {
	return &TaskHandler{
		queue:       queue,
		eventRepo:   eventRepo,
		idempotency: id,
		logger:      logger,
		validator:   validator,
	}
}

func (h *TaskHandler) HandleCancelTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if id == "" {
		sender.RespondWithError(w, http.StatusBadRequest, "task id is required", nil)
		return
	}

	if err := h.eventRepo.CancelTask(ctx, id); err != nil {
		sender.RespondWithError(w, http.StatusConflict, err.Error(), err)
		return
	}

	sender.RespondWithJSON(w, http.StatusOK, map[string]any{
		"status":   "cancelled",
		"event_id": id,
	})
}

func (h *TaskHandler) HandleCreateTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	traceID := r.Context().Value(middleware.TraceIDKey).(string)
	if traceID == "" {
		traceID = "no-trace-id"
	}

	var req TaskRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sender.RespondWithError(w, http.StatusBadRequest, "invalid body", err)
		return
	}

	// -----------------------------
	// Basic validation
	// -----------------------------
	if req.Type == "" {
		sender.RespondWithError(w, http.StatusBadRequest, "type is required", errors.New("missing type"))
		return
	}

	if len(req.Payload) == 0 {
		sender.RespondWithError(w, http.StatusBadRequest, "payload is required", errors.New("missing payload"))
		return
	}

	if req.ExecuteAt != nil && req.ExecuteAt.Before(time.Now()) {
		sender.RespondWithError(w, http.StatusBadRequest, "Executes_at must be in the future", errors.New("Executes at must be in the future"))
		return
	}

	if req.Priority == "" {
		req.Priority = "medium"
	}

	// -----------------------------
	// Validate main task
	// -----------------------------
	if err := h.validator.Validate(req.Type, req.Payload); err != nil {
		sender.RespondWithError(w, http.StatusBadRequest, "invalid payload", err)
		return
	}

	// -----------------------------
	// Validate next ONLY if exists
	// -----------------------------
	if req.Next != nil {
		if req.Next.Type == "" {
			sender.RespondWithError(w, http.StatusBadRequest, "next.type is required", nil)
			return
		}

		if len(req.Next.Payload) > 0 {
			if err := h.validator.Validate(req.Next.Type, req.Next.Payload); err != nil {
				sender.RespondWithError(w, http.StatusBadRequest, "invalid next payload", err)
				return
			}
		}
	}

	h.logger.Info("creating task",
		"trace_id", traceID,
		"type", req.Type,
		"priority", req.Priority,
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

	// Save event — marshal the full request so the worker can read Next
	fullPayload, err := json.Marshal(req)
	if err != nil {
		sender.RespondWithError(w, http.StatusInternalServerError, "failed to marshal request", err)
		return
	}
	err = h.eventRepo.SaveProcessedEvent(ctx, eventID, req.Type, string(fullPayload), "pending", traceID, req.Priority, "")
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
	if req.ExecuteAt != nil {
		if err := h.queue.Schedule(eventID, req.Priority, *req.ExecuteAt); err != nil {
			h.eventRepo.UpdateEventStatus(ctx, eventID, "failed")
			sender.RespondWithError(w, http.StatusInternalServerError, "Failed to enqueue", err)
			return
		}

	} else {
		if err := h.queue.EnqueueWithPriority(eventID, req.Priority); err != nil {
			h.eventRepo.UpdateEventStatus(ctx, eventID, "failed")
			sender.RespondWithError(w, http.StatusInternalServerError, "Failed to enqueue with priority", err)
			return
		}
	}

	// Send Success
	sender.RespondWithJSON(w, http.StatusOK, map[string]any{
		"status":   "accepted",
		"event_id": eventID,
	})

}
