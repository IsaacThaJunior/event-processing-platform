package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/isaacthajunior/mid-prod/internal/database"
	"github.com/isaacthajunior/mid-prod/internal/domain"
	"github.com/isaacthajunior/mid-prod/internal/repository"
	"github.com/isaacthajunior/mid-prod/internal/sender"
)

type AdminHandler struct {
	adminRepo  *repository.AdminRepository
	queue      domain.Queue
	workerPool domain.WorkerHealthProvider
	logger     *slog.Logger
}

func NewAdminHandler(
	adminRepo *repository.AdminRepository,
	queue domain.Queue,
	workerPool domain.WorkerHealthProvider,
	logger *slog.Logger,
) *AdminHandler {
	return &AdminHandler{
		adminRepo:  adminRepo,
		queue:      queue,
		workerPool: workerPool,
		logger:     logger,
	}
}

// --- response types ---

type taskResponse struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	Priority  string `json:"priority,omitempty"`
	ParentID  string `json:"parent_id,omitempty"`
	TraceID   string `json:"trace_id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type retryLogResponse struct {
	Attempt      int32  `json:"attempt"`
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message,omitempty"`
	CreatedAt    string `json:"created_at"`
}

func toTaskResponse(e database.Event) taskResponse {
	r := taskResponse{
		ID:      e.ID,
		Type:    e.Type,
		TraceID: e.TraceID,
	}
	if e.Status.Valid {
		r.Status = e.Status.String
	}
	if e.Priority.Valid {
		r.Priority = e.Priority.String
	}
	if e.Parentid.Valid {
		r.ParentID = e.Parentid.String
	}
	if e.CreatedAt.Valid {
		r.CreatedAt = e.CreatedAt.Time.Format("2006-01-02T15:04:05Z")
	}
	if e.UpdatedAt.Valid {
		r.UpdatedAt = e.UpdatedAt.Time.Format("2006-01-02T15:04:05Z")
	}
	return r
}

// --- handlers ---

// GET /api/admin/dashboard/stats
func (h *AdminHandler) HandleDashboardStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	statusCounts, err := h.adminRepo.GetStatusCounts(ctx)
	if err != nil {
		sender.RespondWithError(w, http.StatusInternalServerError, "failed to get status counts", err)
		return
	}

	recent, err := h.adminRepo.GetRecentProcessedCount(ctx)
	if err != nil {
		sender.RespondWithError(w, http.StatusInternalServerError, "failed to get recent count", err)
		return
	}

	depths, err := h.queue.GetQueueDepths()
	if err != nil {
		sender.RespondWithError(w, http.StatusInternalServerError, "failed to get queue depths", err)
		return
	}

	byStatus := make(map[string]int64, len(statusCounts))
	var total int64
	for _, sc := range statusCounts {
		byStatus[sc.Status] = sc.Count
		total += sc.Count
	}

	sender.RespondWithJSON(w, http.StatusOK, map[string]any{
		"total_tasks":        total,
		"by_status":          byStatus,
		"processed_last_24h": recent,
		"queue_depths":       depths,
		"worker_health":      h.workerPool.HealthStats(),
	})
}

// GET /api/admin/tasks?page=1&page_size=20&status=&type=&priority=&search=
func (h *AdminHandler) HandleListTasks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	params := repository.ListEventsParams{
		Status:   q.Get("status"),
		Type:     q.Get("type"),
		Priority: q.Get("priority"),
		Search:   q.Get("search"),
		Page:     page,
		PageSize: pageSize,
	}

	events, total, err := h.adminRepo.ListEvents(ctx, params)
	if err != nil {
		sender.RespondWithError(w, http.StatusInternalServerError, "failed to list tasks", err)
		return
	}

	tasks := make([]taskResponse, 0, len(events))
	for _, e := range events {
		tasks = append(tasks, toTaskResponse(e))
	}

	totalPages := int(math.Ceil(float64(total) / float64(pageSize)))

	sender.RespondWithJSON(w, http.StatusOK, map[string]any{
		"tasks":       tasks,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
	})
}

// GET /api/admin/tasks/{id}
func (h *AdminHandler) HandleGetTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if id == "" {
		sender.RespondWithError(w, http.StatusBadRequest, "task id is required", nil)
		return
	}

	event, err := h.adminRepo.GetEventByID(ctx, id)
	if err != nil {
		sender.RespondWithError(w, http.StatusNotFound, "task not found", err)
		return
	}

	sender.RespondWithJSON(w, http.StatusOK, toTaskResponse(event))
}

// GET /api/admin/tasks/{id}/retries
func (h *AdminHandler) HandleGetTaskRetries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if id == "" {
		sender.RespondWithError(w, http.StatusBadRequest, "task id is required", nil)
		return
	}

	logs, err := h.adminRepo.GetRetryHistory(ctx, id)
	if err != nil {
		sender.RespondWithError(w, http.StatusInternalServerError, "failed to get retry history", err)
		return
	}

	retries := make([]retryLogResponse, 0, len(logs))
	for _, l := range logs {
		entry := retryLogResponse{
			Attempt: l.Attempt,
			Status:  l.Status,
		}
		if l.ErrorMessage.Valid {
			entry.ErrorMessage = l.ErrorMessage.String
		}
		if l.CreatedAt.Valid {
			entry.CreatedAt = l.CreatedAt.Time.Format("2006-01-02T15:04:05Z")
		}
		retries = append(retries, entry)
	}

	sender.RespondWithJSON(w, http.StatusOK, map[string]any{
		"task_id": id,
		"retries": retries,
	})
}

// POST /api/admin/tasks/{id}/retry
func (h *AdminHandler) HandleRetryTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if id == "" {
		sender.RespondWithError(w, http.StatusBadRequest, "task id is required", nil)
		return
	}

	priority, err := h.adminRepo.ResetTaskForRetry(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotRetryable) {
			sender.RespondWithError(w, http.StatusConflict, err.Error(), err)
			return
		}
		sender.RespondWithError(w, http.StatusInternalServerError, "failed to reset task", err)
		return
	}

	if err := h.queue.EnqueueWithPriority(id, priority); err != nil {
		sender.RespondWithError(w, http.StatusInternalServerError, "task reset but failed to re-enqueue", err)
		return
	}

	sender.RespondWithJSON(w, http.StatusOK, map[string]any{
		"status":   "queued",
		"event_id": id,
		"priority": priority,
	})
}

// GET /api/admin/dlq
func (h *AdminHandler) HandleListDLQ(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ids, err := h.queue.GetDLQItems()
	if err != nil {
		sender.RespondWithError(w, http.StatusInternalServerError, "failed to list DLQ", err)
		return
	}

	tasks := make([]taskResponse, 0, len(ids))
	for _, id := range ids {
		event, err := h.adminRepo.GetEventByID(ctx, id)
		if err != nil {
			continue
		}
		tasks = append(tasks, toTaskResponse(event))
	}

	sender.RespondWithJSON(w, http.StatusOK, map[string]any{
		"total": len(ids),
		"tasks": tasks,
	})
}

// POST /api/admin/dlq/{id}/retry
func (h *AdminHandler) HandleRetryDLQTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if id == "" {
		sender.RespondWithError(w, http.StatusBadRequest, "task id is required", nil)
		return
	}

	if err := h.queue.RemoveFromDLQ(id); err != nil {
		sender.RespondWithError(w, http.StatusNotFound, "task not found in DLQ", err)
		return
	}

	priority, err := h.adminRepo.ResetTaskForRetry(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotRetryable) {
			sender.RespondWithError(w, http.StatusConflict, err.Error(), err)
			return
		}
		sender.RespondWithError(w, http.StatusInternalServerError, "failed to reset task", err)
		return
	}

	if err := h.queue.EnqueueWithPriority(id, priority); err != nil {
		sender.RespondWithError(w, http.StatusInternalServerError, "task reset but failed to re-enqueue", err)
		return
	}

	sender.RespondWithJSON(w, http.StatusOK, map[string]any{
		"status":   "queued",
		"event_id": id,
		"priority": priority,
	})
}

// DELETE /api/admin/dlq/{id}
func (h *AdminHandler) HandleRemoveDLQTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		sender.RespondWithError(w, http.StatusBadRequest, "task id is required", nil)
		return
	}

	if err := h.queue.RemoveFromDLQ(id); err != nil {
		sender.RespondWithError(w, http.StatusNotFound, "task not found in DLQ", err)
		return
	}

	sender.RespondWithJSON(w, http.StatusOK, map[string]any{
		"status":   "removed",
		"event_id": id,
	})
}

// POST /api/admin/tasks/{id}/requeue — re-enqueues a pending task orphaned from Redis
func (h *AdminHandler) HandleRequeueTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if id == "" {
		sender.RespondWithError(w, http.StatusBadRequest, "task id is required", nil)
		return
	}

	event, err := h.adminRepo.GetEventByID(ctx, id)
	if err != nil {
		sender.RespondWithError(w, http.StatusNotFound, "task not found", err)
		return
	}

	if !event.Status.Valid || event.Status.String != "pending" {
		sender.RespondWithError(w, http.StatusConflict, "only pending tasks can be requeued", nil)
		return
	}

	// Parse the stored payload to recover execute_at and priority.
	var req struct {
		Priority  string     `json:"priority"`
		ExecuteAt *time.Time `json:"execute_at"`
	}
	_ = json.Unmarshal([]byte(event.Payload), &req)

	priority := req.Priority
	if priority == "" {
		if event.Priority.Valid {
			priority = event.Priority.String
		} else {
			priority = "medium"
		}
	}

	if req.ExecuteAt != nil && req.ExecuteAt.After(time.Now()) {
		if err := h.queue.Schedule(id, priority, *req.ExecuteAt); err != nil {
			sender.RespondWithError(w, http.StatusInternalServerError, "failed to schedule task", err)
			return
		}
		sender.RespondWithJSON(w, http.StatusOK, map[string]any{
			"status":     "scheduled",
			"event_id":   id,
			"priority":   priority,
			"execute_at": req.ExecuteAt,
		})
		return
	}

	if err := h.queue.EnqueueWithPriority(id, priority); err != nil {
		sender.RespondWithError(w, http.StatusInternalServerError, "failed to enqueue task", err)
		return
	}

	sender.RespondWithJSON(w, http.StatusOK, map[string]any{
		"status":   "queued",
		"event_id": id,
		"priority": priority,
	})
}

// GET /api/admin/queue/depth
func (h *AdminHandler) HandleQueueDepth(w http.ResponseWriter, r *http.Request) {
	depths, err := h.queue.GetQueueDepths()
	if err != nil {
		sender.RespondWithError(w, http.StatusInternalServerError, "failed to get queue depths", err)
		return
	}
	sender.RespondWithJSON(w, http.StatusOK, depths)
}

// GET /api/admin/workers/health
func (h *AdminHandler) HandleWorkerHealth(w http.ResponseWriter, r *http.Request) {
	sender.RespondWithJSON(w, http.StatusOK, h.workerPool.HealthStats())
}
