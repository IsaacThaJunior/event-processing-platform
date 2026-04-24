package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/isaacthajunior/mid-prod/internal/handler"
	"github.com/isaacthajunior/mid-prod/internal/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type server struct {
	adminHandler *handler.AdminHandler
	taskHandler  *handler.TaskHandler
	cancel       context.CancelFunc
	logger       *slog.Logger
	httpServer   *http.Server
	port         int
}

func newServer(adminHandler *handler.AdminHandler, taskHandler *handler.TaskHandler, cancel context.CancelFunc, logger *slog.Logger, port int) *server {
	s := &server{
		adminHandler: adminHandler,
		taskHandler:  taskHandler,
		cancel:       cancel,
		logger:       logger,
		httpServer:   nil,
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	// Task API
	mux.HandleFunc("POST /tasks", s.taskHandler.HandleCreateTask)
	mux.HandleFunc("DELETE /tasks/{id}", s.taskHandler.HandleCancelTask)

	// Admin API
	mux.HandleFunc("GET /api/admin/dashboard/stats", s.adminHandler.HandleDashboardStats)
	mux.HandleFunc("GET /api/admin/tasks", s.adminHandler.HandleListTasks)
	mux.HandleFunc("GET /api/admin/tasks/{id}", s.adminHandler.HandleGetTask)
	mux.HandleFunc("GET /api/admin/tasks/{id}/retries", s.adminHandler.HandleGetTaskRetries)
	mux.HandleFunc("POST /api/admin/tasks/{id}/retry", s.adminHandler.HandleRetryTask)
	mux.HandleFunc("POST /api/admin/tasks/{id}/requeue", s.adminHandler.HandleRequeueTask)
	mux.HandleFunc("GET /api/admin/dlq", s.adminHandler.HandleListDLQ)
	mux.HandleFunc("POST /api/admin/dlq/{id}/retry", s.adminHandler.HandleRetryDLQTask)
	mux.HandleFunc("DELETE /api/admin/dlq/{id}", s.adminHandler.HandleRemoveDLQTask)
	mux.HandleFunc("GET /api/admin/queue/depth", s.adminHandler.HandleQueueDepth)
	mux.HandleFunc("GET /api/admin/workers/health", s.adminHandler.HandleWorkerHealth)

	mux.Handle("/metrics", promhttp.Handler())
	handler := middleware.EnableCORS(middleware.TraceMiddleware(middleware.RequestLogger(logger)(mux)))

	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: handler,
	}

	return s
}

func (s *server) start() error {
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return err
	}

	addr := ln.Addr()
	tcpAddr := addr.(*net.TCPAddr)

	s.logger.Debug("Debugging", "message", "Event App is running on http://localhost", "port", tcpAddr.Port)

	if err := s.httpServer.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *server) shutdown(ctx context.Context) error {
	s.logger.Debug("Event app is shutting down")
	return s.httpServer.Shutdown(ctx)
}
