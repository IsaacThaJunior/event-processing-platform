package main

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/isaacthajunior/mid-prod/internal/handler"
	midware "github.com/isaacthajunior/mid-prod/internal/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func newServer(adminHandler *handler.AdminHandler, taskHandler *handler.TaskHandler, logger *slog.Logger) http.Handler {
	r := chi.NewRouter()

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000", "http://localhost:5173"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// otelhttp must be first so the OTel span is in context before TraceMiddleware reads it
	r.Use(otelhttp.NewMiddleware("event-app"))
	r.Use(midware.TraceMiddleware)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(midware.RequestLogger(logger))
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hi there. Everything is a-ok"))
	})

	// Task API
	r.Post("/tasks", taskHandler.HandleCreateTask)
	r.Delete("/tasks/{id}", taskHandler.HandleCancelTask)

	// Admin API
	r.Route("/api/admin", func(r chi.Router) {
		r.Get("/dashboard/stats", adminHandler.HandleDashboardStats)
		r.Get("/tasks", adminHandler.HandleListTasks)
		r.Get("/tasks/{id}", adminHandler.HandleGetTask)
		r.Get("/tasks/{id}/retries", adminHandler.HandleGetTaskRetries)
		r.Post("/tasks/{id}/retry", adminHandler.HandleRetryTask)
		r.Post("/tasks/{id}/requeue", adminHandler.HandleRequeueTask)
		r.Get("/dlq", adminHandler.HandleListDLQ)
		r.Post("/dlq/{id}/retry", adminHandler.HandleRetryDLQTask)
		r.Delete("/dlq/{id}", adminHandler.HandleRemoveDLQTask)
		r.Get("/queue/depth", adminHandler.HandleQueueDepth)
		r.Get("/workers/health", adminHandler.HandleWorkerHealth)
	})

	r.Handle("/metrics", promhttp.Handler())

	return r
}
