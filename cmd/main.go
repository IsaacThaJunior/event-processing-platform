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

	// --- Worker pool ---
	workerPool := worker.NewWorkerPool(queue, eventRepo, 3, logger)
	workerPool.Start()
	defer workerPool.Stop()

	metrics.Init()

	// Task handler
	taskHandler := handler.NewTaskHanler(queue, eventRepo, idempotencyService, logger)

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("/test-process", func(w http.ResponseWriter, r *http.Request) {
		metrics.TasksProcessed.WithLabelValues("test").Inc()
		metrics.TasksProcessed.WithLabelValues("email").Inc()
		metrics.TasksProcessed.WithLabelValues("payment").Inc()
		metrics.TasksRetried.WithLabelValues("test").Inc()
		metrics.TasksFailed.WithLabelValues("test").Inc()

		w.Write([]byte("Test metrics incremented!\n"))
	})

	mux.HandleFunc("POST /tasks", taskHandler.HandleCreateTask)

	mux.Handle("/metrics", promhttp.Handler())

	http.ListenAndServe(":8080", middleware.TraceMiddleware(mux))
}
