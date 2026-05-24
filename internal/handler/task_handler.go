package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"

	"github.com/isaacthajunior/mid-prod/internal/domain"
	"github.com/isaacthajunior/mid-prod/internal/middleware"
	"github.com/isaacthajunior/mid-prod/internal/repository"
	"github.com/isaacthajunior/mid-prod/internal/sender"
	"github.com/isaacthajunior/mid-prod/internal/service"
	"github.com/isaacthajunior/mid-prod/internal/storage"
)

var taskTracer = otel.Tracer("handler.task")

type TaskHandler struct {
	queue       domain.Queue
	eventRepo   repository.EventRepository
	idempotency *service.IdempotencyRepo
	validator   *service.TaskValidator
	storage     *storage.Client
}

type TaskRequest struct {
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Priority  string          `json:"priority"`
	ExecuteAt *time.Time      `json:"execute_at,omitempty"`
	Next      *TaskRequest    `json:"next,omitempty"`
	// TraceContext carries the W3C traceparent so workers continue this trace.
	TraceContext string `json:"trace_context,omitempty"`
}

func NewTaskHanler(queue domain.Queue, eventRepo repository.EventRepository, id *service.IdempotencyRepo, validator *service.TaskValidator, storageClient *storage.Client) *TaskHandler {
	return &TaskHandler{
		queue:       queue,
		eventRepo:   eventRepo,
		idempotency: id,
		validator:   validator,
		storage:     storageClient,
	}
}

func (h *TaskHandler) HandleCreateTask(w http.ResponseWriter, r *http.Request) {
	ctx, span := taskTracer.Start(r.Context(), "HandleCreateTask")
	defer span.End()

	logCtx := middleware.GetLogContext(ctx)
	traceID, _ := ctx.Value(middleware.TraceIDKey).(string)

	var req TaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "decode request body")
		logCtx.AddEvent("decode_request_body", "failed", err)
		sender.RespondWithError(ctx, w, http.StatusBadRequest, err)
		return
	}

	// --- validation ---
	if req.Type == "" {
		logCtx.AddEvent("request_type_empty", "failed", fmt.Errorf("missing type"))
		sender.RespondWithError(ctx, w, http.StatusBadRequest, fmt.Errorf("missing type"))
		return
	}
	if len(req.Payload) == 0 {
		logCtx.AddEvent("request_payload_empty", "failed", fmt.Errorf("missing payload"))
		sender.RespondWithError(ctx, w, http.StatusBadRequest, fmt.Errorf("missing payload"))
		return
	}
	if req.ExecuteAt != nil && req.ExecuteAt.Before(time.Now()) {
		logCtx.AddEvent("past_executes_at_time", "failed", fmt.Errorf("execute_at must be in the future"))
		sender.RespondWithError(ctx, w, http.StatusBadRequest, fmt.Errorf("execute_at must be in the future"))
		return
	}
	if req.Priority == "" {
		req.Priority = "medium"
	}
	if err := h.validator.Validate(req.Type, req.Payload); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "validation failed")
		logCtx.AddEvent("type_and_payload_validator", "failed", err)
		sender.RespondWithError(ctx, w, http.StatusBadRequest, err)
		return
	}
	if req.Next != nil {
		if req.Next.Type == "" {
			logCtx.AddEvent("next_type_empty", "failed", fmt.Errorf("Next type is empty"))
			sender.RespondWithError(ctx, w, http.StatusBadRequest, fmt.Errorf("Next type is empty"))
			return
		}
		if len(req.Next.Payload) > 0 {
			if err := h.validator.Validate(req.Next.Type, req.Next.Payload); err != nil {
				logCtx.AddEvent("next_payload_empty", "failed", err)
				sender.RespondWithError(ctx, w, http.StatusBadRequest, err)
				return
			}
		}
	}

	logCtx.AddEvent("passed_all_validation_checks", "success", nil)
	logCtx.TaskType = req.Type
	logCtx.Priority = req.Priority
	span.SetAttributes(
		attribute.String("task.type", req.Type),
		attribute.String("task.priority", req.Priority),
	)

	// --- idempotency check ---
	idemCtx, idemSpan := taskTracer.Start(ctx, "check-idempotency")
	key := h.idempotency.GenerateIdempotencyKey(req.Type, string(req.Payload), req.Priority)
	processed, existingEventID, err := h.idempotency.Isprocessed(idemCtx, key)
	idemSpan.End()
	if err != nil {
		span.RecordError(err)
		logCtx.AddEvent("failed_idempotency_check", "failed", err)
		sender.RespondWithError(ctx, w, http.StatusInternalServerError, err)
		return
	}
	if processed {
		logCtx.AddEvent("duplicate_event", "success", nil)
		sender.RespondWithJSON(w, http.StatusConflict, map[string]any{
			"status":   "duplicate",
			"event_id": existingEventID,
		})
		return
	}

	eventID := uuid.New().String()
	logCtx.EventID = eventID
	span.SetAttributes(attribute.String("task.id", eventID))

	// Inject W3C traceparent so the worker continues this trace
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	req.TraceContext = carrier["traceparent"]

	// --- persist ---
	_, dbSpan := taskTracer.Start(ctx, "save-task-to-db")
	fullPayload, err := json.Marshal(req)
	if err != nil {
		dbSpan.RecordError(err)
		dbSpan.End()
		logCtx.AddEvent("failed_marshalling", "failed", err)
		sender.RespondWithError(ctx, w, http.StatusInternalServerError, err)
		return
	}
	err = h.eventRepo.SaveProcessedEvent(ctx, eventID, req.Type, string(fullPayload), "pending", traceID, req.Priority, "", req.ExecuteAt)
	dbSpan.End()
	if err != nil {
		span.RecordError(err)
		logCtx.AddEvent("failed_db_saving", "failed", err)
		sender.RespondWithError(ctx, w, http.StatusInternalServerError, err)
		return
	}

	meta := &service.IdempotencyMetadata{Command: req.Type, Source: "api"}
	_, err = h.idempotency.CheckAndRecordToDB(ctx, key, eventID, meta)
	if err != nil {
		span.RecordError(err)
		logCtx.AddEvent("failed_db_inserting", "failed", err)
		sender.RespondWithError(ctx, w, http.StatusInternalServerError, err)
		return
	}

	// --- enqueue ---
	_, enqSpan := taskTracer.Start(ctx, "enqueue-task")
	if req.ExecuteAt != nil {
		err = h.queue.Schedule(eventID, req.Priority, *req.ExecuteAt)
	} else {
		err = h.queue.EnqueueWithPriority(eventID, req.Priority)
	}
	enqSpan.End()
	if err != nil {
		span.RecordError(err)
		logCtx.AddEvent("failed_enqueue", "failed", err)
		logCtx.Status = "failed"
		h.eventRepo.UpdateEventStatus(ctx, eventID, "failed")
		sender.RespondWithError(ctx, w, http.StatusInternalServerError, err)
		return
	}

	resp := map[string]any{"status": "accepted", "event_id": eventID}
	if req.ExecuteAt != nil {
		resp["scheduled_at"] = req.ExecuteAt
	}
	sender.RespondWithJSON(w, http.StatusOK, resp)
}

func (h *TaskHandler) HandleCancelTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logCtx := middleware.GetLogContext(ctx)

	id := chi.URLParam(r, "id")
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

// GET /tasks/{id}/result — returns the output of a completed task.
// File-producing tasks (resize_image, generate_report) return a presigned download URL.
// Non-file tasks (send_email) return their delivery data directly.
func (h *TaskHandler) HandleGetTaskResult(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	event, err := h.eventRepo.GetEventByID(ctx, id)
	if err != nil {
		sender.RespondWithError(ctx, w, http.StatusNotFound, fmt.Errorf("task not found"))
		return
	}

	if !event.Status.Valid || event.Status.String != "processed" {
		sender.RespondWithJSON(w, http.StatusAccepted, map[string]any{
			"event_id": id,
			"status":   event.Status.String,
			"message":  "task not yet processed",
		})
		return
	}

	if !event.Result.Valid || event.Result.String == "" {
		sender.RespondWithError(ctx, w, http.StatusNotFound, fmt.Errorf("no result for this task"))
		return
	}

	// All results are stored as {"kind": "...", ...} so handlers can evolve independently.
	var result map[string]any
	if err := json.Unmarshal([]byte(event.Result.String), &result); err != nil {
		sender.RespondWithError(ctx, w, http.StatusInternalServerError, fmt.Errorf("malformed result"))
		return
	}

	switch result["kind"] {
	case "file":
		key, _ := result["key"].(string)
		if h.storage == nil {
			sender.RespondWithError(ctx, w, http.StatusServiceUnavailable, fmt.Errorf("storage not configured"))
			return
		}
		url, err := h.storage.PresignedURL(ctx, key)
		if err != nil {
			sender.RespondWithError(ctx, w, http.StatusInternalServerError, err)
			return
		}
		sender.RespondWithJSON(w, http.StatusOK, map[string]any{
			"event_id":   id,
			"kind":       "file",
			"result_url": url,
			"expires_in": "24h",
		})

	default:
		// Non-file results (e.g. email delivery) — return the data as-is.
		result["event_id"] = id
		sender.RespondWithJSON(w, http.StatusOK, result)
	}
}
