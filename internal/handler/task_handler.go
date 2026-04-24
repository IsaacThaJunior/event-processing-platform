package handler

import (
	"encoding/json"
	"fmt"
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
	validator   *service.TaskValidator
}

type TaskRequest struct {
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Priority  string          `json:"priority"`
	ExecuteAt *time.Time      `json:"execute_at,omitempty"`

	Next *TaskRequest `json:"next,omitempty"`
}

func NewTaskHanler(queue domain.Queue, eventRepo repository.EventRepository, id *service.IdempotencyRepo, validator *service.TaskValidator) *TaskHandler {
	return &TaskHandler{
		queue:       queue,
		eventRepo:   eventRepo,
		idempotency: id,
		validator:   validator,
	}
}

func (h *TaskHandler) HandleCancelTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logCtx := middleware.GetLogContext(ctx)

	id := r.PathValue("id")
	if id == "" {
		logCtx.AddEvent(
			"no_id_in_request",
			"failed",
			fmt.Errorf("Id required"),
		)
		sender.RespondWithError(ctx, w, http.StatusBadRequest, fmt.Errorf("Id required"))
		return
	}

	if err := h.eventRepo.CancelTask(ctx, id); err != nil {
		logCtx.AddEvent(
			"cancel_task_error",
			"failed",
			err,
		)
		sender.RespondWithError(ctx, w, http.StatusConflict, err)
		return
	}

	sender.RespondWithJSON(w, http.StatusOK, map[string]any{
		"status":   "cancelled",
		"event_id": id,
	})
}

func (h *TaskHandler) HandleCreateTask(w http.ResponseWriter, r *http.Request) {
	// Create the logContext so we can add to it
	ctx := r.Context()
	logCtx := middleware.GetLogContext(ctx)
	traceID := r.Context().Value(middleware.TraceIDKey).(string)
	if traceID == "" {
		traceID = "no-trace-id"
	}

	var req TaskRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logCtx.AddEvent(
			"decode_request_body",
			"failed",
			err,
		)
		sender.RespondWithError(ctx, w, http.StatusBadRequest, err)
		return
	}

	// -----------------------------
	// Basic validation
	// -----------------------------
	if req.Type == "" {
		logCtx.AddEvent(
			"request_type_empty",
			"failed",
			fmt.Errorf("missing type"),
		)
		sender.RespondWithError(ctx, w, http.StatusBadRequest, fmt.Errorf("missing type"))
		return
	}

	if len(req.Payload) == 0 {
		logCtx.AddEvent(
			"request_payload_empty",
			"failed",
			fmt.Errorf("missing payload"),
		)
		sender.RespondWithError(ctx, w, http.StatusBadRequest, fmt.Errorf("missing payload"))
		return
	}

	if req.ExecuteAt != nil && req.ExecuteAt.Before(time.Now()) {
		logCtx.AddEvent(
			"past_executes_at_time",
			"failed",
			fmt.Errorf("Executes at must be in the future"),
		)
		sender.RespondWithError(ctx, w, http.StatusBadRequest, fmt.Errorf("Executes at must be in the future"))
		return
	}

	if req.Priority == "" {
		req.Priority = "medium"
	}

	// -----------------------------
	// Validate main task
	// -----------------------------
	if err := h.validator.Validate(req.Type, req.Payload); err != nil {
		logCtx.AddEvent(
			"type_and_payload_validator",
			"failed",
			err,
		)
		sender.RespondWithError(ctx, w, http.StatusBadRequest, err)
		return
	}

	// -----------------------------
	// Validate next ONLY if exists
	// -----------------------------
	if req.Next != nil {
		if req.Next.Type == "" {
			logCtx.AddEvent(
				"next_type_empty",
				"failed",
				fmt.Errorf("Next type is empty"),
			)
			sender.RespondWithError(ctx, w, http.StatusBadRequest, fmt.Errorf("Next type is empty"))
			return
		}

		if len(req.Next.Payload) > 0 {
			if err := h.validator.Validate(req.Next.Type, req.Next.Payload); err != nil {
				logCtx.AddEvent(
					"next_payload_empty",
					"failed",
					err,
				)
				sender.RespondWithError(ctx, w, http.StatusBadRequest, err)
				return
			}
		}
	}

	logCtx.AddEvent(
		"passed_all_validation_checks",
		"success",
		nil,
	)
	logCtx.TaskType = req.Type
	logCtx.Priority = req.Priority

	key := h.idempotency.GenerateIdempotencyKey(req.Type, string(req.Payload), req.Priority)

	processed, existingEventID, err := h.idempotency.Isprocessed(ctx, key)
	if err != nil {
		logCtx.AddEvent(
			"failed_idempotency_check",
			"failed",
			err,
		)
		sender.RespondWithError(ctx, w, http.StatusInternalServerError, err)
		return
	}

	if processed {
		logCtx.AddEvent(
			"duplicate_event",
			"success",
			nil,
		)
		sender.RespondWithJSON(w, http.StatusConflict, map[string]any{
			"status":   "duplicate",
			"event_id": existingEventID,
		})
		return
	}

	eventID := uuid.New().String()
	logCtx.EventID = eventID

	// Save event — marshal the full request so the worker can read Next
	fullPayload, err := json.Marshal(req)
	if err != nil {
		logCtx.AddEvent(
			"failed_marshalling",
			"failed",
			err,
		)
		sender.RespondWithError(ctx, w, http.StatusInternalServerError, err)
		return
	}
	err = h.eventRepo.SaveProcessedEvent(ctx, eventID, req.Type, string(fullPayload), "pending", traceID, req.Priority, "")
	if err != nil {
		logCtx.AddEvent(
			"failed_db_saving",
			"failed",
			err,
		)
		sender.RespondWithError(ctx, w, http.StatusInternalServerError, err)
		return
	}

	// Record idempotency
	meta := &service.IdempotencyMetadata{
		Command: req.Type,
		Source:  "api",
	}
	_, err = h.idempotency.CheckAndRecordToDB(ctx, key, eventID, meta)
	if err != nil {
		logCtx.AddEvent(
			"failed_db_inserting",
			"failed",
			err,
		)
		sender.RespondWithError(ctx, w, http.StatusInternalServerError, err)
		return
	}

	// Enqueue
	if req.ExecuteAt != nil {
		if err := h.queue.Schedule(eventID, req.Priority, *req.ExecuteAt); err != nil {
			logCtx.AddEvent(
				"failed_db_inserting",
				"failed",
				err,
			)
			logCtx.Status = "failed"
			h.eventRepo.UpdateEventStatus(ctx, eventID, "failed")
			sender.RespondWithError(ctx, w, http.StatusInternalServerError, err)
			return
		}

	} else {
		if err := h.queue.EnqueueWithPriority(eventID, req.Priority); err != nil {
			logCtx.AddEvent(
				"failed_enqueing_for_scheduled",
				"failed",
				err,
			)
			logCtx.Status = "failed"
			h.eventRepo.UpdateEventStatus(ctx, eventID, "failed")
			sender.RespondWithError(ctx, w, http.StatusInternalServerError, err)
			return
		}
	}

	// Send Success
	sender.RespondWithJSON(w, http.StatusOK, map[string]any{
		"status":   "accepted",
		"event_id": eventID,
	})

}
