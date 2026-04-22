package main

import (
	"log"
	"log/slog"
	"net/http"

	"github.com/isaacthajunior/mid-prod/internal/database"
	"github.com/isaacthajunior/mid-prod/internal/handler"
	"github.com/isaacthajunior/mid-prod/internal/metrics"
	"github.com/isaacthajunior/mid-prod/internal/middleware"
	"github.com/isaacthajunior/mid-prod/internal/repository"
	"github.com/isaacthajunior/mid-prod/internal/service"
	"github.com/isaacthajunior/mid-prod/internal/worker"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	logger := slog.Default()
	pool, err := database.NewPool()
	if err != nil {
		log.Fatalf("failed to connect to db: %v", err)
	}
	log.Println("Connected to Postgres successfully")

	defer pool.Close()

	// Initialize queries from DB
	queries := database.New(pool)

	// Create event repo
	eventRepo := repository.NewEventRepository(queries)

	// Create the idempotency service
	idempotencyService := service.NewIdempotencyService(queries, pool)

	// This for Redis Client
	redisClient := repository.NewRedisClient()
	defer redisClient.Close()

	// This is for Redis queue
	queue := repository.NewRedisQueue(redisClient, "events_queue")
	validator := service.NewTaskValidator()

	// --- Worker pool ---
	workerPool := worker.NewWorkerPool(queue, eventRepo, 3, logger, validator)
	workerPool.Start()
	defer workerPool.Stop()

	metrics.Init()

	// Task handler
	taskHandler := handler.NewTaskHanler(queue, eventRepo, idempotencyService, logger, validator)

	// Admin handler
	adminRepo := repository.NewAdminRepository(queries)
	adminHandler := handler.NewAdminHandler(adminRepo, queue, workerPool, logger)

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	// Task API
	mux.HandleFunc("POST /tasks", taskHandler.HandleCreateTask)
	mux.HandleFunc("DELETE /tasks/{id}", taskHandler.HandleCancelTask)

	// Admin API
	mux.HandleFunc("GET /api/admin/dashboard/stats", adminHandler.HandleDashboardStats)
	mux.HandleFunc("GET /api/admin/tasks", adminHandler.HandleListTasks)
	mux.HandleFunc("GET /api/admin/tasks/{id}", adminHandler.HandleGetTask)
	mux.HandleFunc("GET /api/admin/tasks/{id}/retries", adminHandler.HandleGetTaskRetries)
	mux.HandleFunc("POST /api/admin/tasks/{id}/retry", adminHandler.HandleRetryTask)
	mux.HandleFunc("GET /api/admin/queue/depth", adminHandler.HandleQueueDepth)
	mux.HandleFunc("GET /api/admin/workers/health", adminHandler.HandleWorkerHealth)

	mux.Handle("/metrics", promhttp.Handler())

	http.ListenAndServe(":8080", middleware.TraceMiddleware(mux))
}
