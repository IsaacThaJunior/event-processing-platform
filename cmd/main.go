package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/isaacthajunior/mid-prod/internal/database"
	"github.com/isaacthajunior/mid-prod/internal/handler"
	"github.com/isaacthajunior/mid-prod/internal/metrics"
	"github.com/isaacthajunior/mid-prod/internal/repository"
	"github.com/isaacthajunior/mid-prod/internal/service"
	"github.com/isaacthajunior/mid-prod/internal/storage"
	"github.com/isaacthajunior/mid-prod/internal/taskerr"
	"github.com/isaacthajunior/mid-prod/internal/telemetry"
	"github.com/isaacthajunior/mid-prod/internal/worker"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
	pkgerr "github.com/pkg/errors"
	"github.com/pressly/goose/v3"
	"gopkg.in/natefinch/lumberjack.v2"
)

func main() {
	httpPort := 8080
	status := run(httpPort)
	os.Exit(status)
}

func run(port int) int {
	logger, closeFunc, err := initializeLogger()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		return 1
	}
	// Registered first so it runs last — log file stays open until everything else is done.
	defer func() {
		if err := closeFunc(); err != nil {
			fmt.Fprintf(os.Stderr, "error closing log file: %v\n", err)
		}
	}()

	otelEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if otelEndpoint == "" {
		otelEndpoint = "otel-collector:4318"
	}
	tp, err := telemetry.NewTracerProvider(context.Background(), otelEndpoint)
	if err != nil {
		logger.Warn("traces disabled — could not reach OTel Collector", "error", err)
	} else {
		defer tp.Shutdown(context.Background())
	}

	// Run migrations before opening the pgxpool — goose needs *sql.DB, not pgxpool.
	migrationDB, err := sql.Open("pgx", os.Getenv("DB_URL"))
	if err != nil {
		log.Fatalf("goose: open db: %v", err)
	}
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatalf("goose: set dialect: %v", err)
	}
	if err := goose.Up(migrationDB, "./sql/schema"); err != nil {
		log.Fatalf("goose: migrations failed: %v", err)
	}
	migrationDB.Close()
	logger.Debug("database migrations applied")

	workerCount := 3
	if v := os.Getenv("WORKER_COUNT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			workerCount = n
		}
	}
	logger.Debug("worker pool configured", "workers", workerCount, "db_max_conns", workerCount*3)

	pool, err := database.NewPool(workerCount)
	if err != nil {
		log.Fatalf("failed to connect to db: %v", err)
	}
	logger.Debug("Connected to Postgres successfully")
	defer pool.Close()

	queries := database.New(pool)
	eventRepo := repository.NewEventRepository(queries)
	idempotencyService := service.NewIdempotencyService(queries, pool)

	redisClient := repository.NewRedisClient(logger)
	defer redisClient.Close()

	queue := repository.NewRedisQueue(redisClient, "events_queue")
	validator := service.NewTaskValidator()

	storageClient, err := storage.NewMinioClient()
	if err != nil {
		logger.Warn("storage unavailable — image tasks will fail", "error", err)
	} else {
		if err := storageClient.EnsureBucket(context.Background()); err != nil {
			logger.Warn("could not ensure minio bucket", "error", err)
		}
	}

	// Worker pool must stop before Redis/Postgres close, so defer it last (runs first).
	workerPool := worker.NewWorkerPool(queue, eventRepo, workerCount, logger, validator, storageClient)
	workerPool.Start()
	defer workerPool.Stop()

	metrics.Init()

	taskHandler := handler.NewTaskHanler(queue, eventRepo, idempotencyService, validator, storageClient)
	adminRepo := repository.NewAdminRepository(queries)
	adminHandler := handler.NewAdminHandler(adminRepo, queue, workerPool)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      newServer(adminHandler, taskHandler, logger),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  time.Minute,
	}

	// Start server in background so we can listen for signals below.
	serverErr := make(chan error, 1)
	go func() {
		logger.Debug("server started", "port", port)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		logger.Error("server error", "error", err)
		return 1
	case sig := <-quit:
		logger.Info("shutdown signal received", "signal", sig.String())
	}

	// Give in-flight HTTP requests up to 10 seconds to complete.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server forced to shutdown", "error", err)
		return 1
	}

	logger.Info("server stopped cleanly")
	// Deferred cleanup (workerPool.Stop, redisClient.Close, pool.Close, tp.Shutdown, closeFunc) runs here.
	return 0
}

type closeFunc func() error

type maxLevelHandler struct {
	max slog.Level
	slog.Handler
}

func (h maxLevelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level <= h.max && h.Handler.Enabled(ctx, level)
}

func initializeLogger() (*slog.Logger, closeFunc, error) {
	isTTY := isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd())
	logFile := "logs/tasks.log"

	// Create the stderr (debug) handler with tint for colorized output
	debugHandler := tint.NewHandler(os.Stderr, &tint.Options{
		Level:       slog.LevelDebug,
		NoColor:     !isTTY, // Disable colors if not in a TTY
		ReplaceAttr: replaceAttr,
	})

	gatedDebugHandler := maxLevelHandler{max: slog.LevelDebug, Handler: debugHandler}

	logger := &lumberjack.Logger{
		Filename:   logFile,
		MaxSize:    3,
		MaxAge:     28,
		MaxBackups: 10,
		LocalTime:  true,
		Compress:   true,
	}

	infoHandler := slog.NewJSONHandler(logger, &slog.HandlerOptions{
		Level:       slog.LevelInfo,
		ReplaceAttr: replaceAttr,
	})

	slogLogger := slog.New(slog.NewMultiHandler(
		gatedDebugHandler,
		infoHandler,
	))

	// Return a close function to close the file later
	closeFn := func() error {
		return logger.Close()
	}

	return slogLogger, closeFn, nil
}

func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if a.Key == "error" {
		err, ok := a.Value.Any().(error)
		if !ok {
			return a
		}

		// Check if this is a multi-error
		var multiErr multiError
		if errors.As(err, &multiErr) {
			// Handle multi-error by grouping each error under numbered keys
			unwrapped := multiErr.Unwrap()
			groupAttrs := make([]slog.Attr, 0, len(unwrapped))

			for i, subErr := range unwrapped {
				// Get attributes for the sub-error
				subAttrs := errorAttrs(subErr)
				// Create a group attribute using GroupAttrs
				errorGroup := slog.GroupAttrs(fmt.Sprintf("error_%d", i+1), subAttrs...)
				groupAttrs = append(groupAttrs, errorGroup)
			}

			return slog.GroupAttrs("errors", groupAttrs...)
		}

		// Handle single error
		return slog.GroupAttrs("error", errorAttrs(err)...)
	}
	return a
}

type stackTracer interface {
	error
	StackTrace() pkgerr.StackTrace
}

// Add this interface in main.go
type multiError interface {
	error
	Unwrap() []error
}

func errorAttrs(err error) []slog.Attr {
	// Always start with the message
	attrs := []slog.Attr{
		slog.String("message", err.Error()),
	}

	// Always append any linkoerr.Attrs from the error chain
	attrs = append(attrs, taskerr.Attrs(err)...)

	// Conditionally append stack trace if present
	var stackErr stackTracer
	if errors.As(err, &stackErr) {
		if filtered := filterStack(fmt.Sprintf("%+v", stackErr.StackTrace())); filtered != "" {
			attrs = append(attrs, slog.String("stack_trace", filtered))
		}
	}

	return attrs
}

// filterStack keeps only frames that belong to this module, dropping stdlib,
// chi, pgx, and other dependency frames that add noise without adding context.
func filterStack(full string) string {
	var kept []string
	lines := strings.Split(full, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "github.com/isaacthajunior/") || strings.HasPrefix(trimmed, "main.") {
			kept = append(kept, line)
			if i+1 < len(lines) {
				i++
				kept = append(kept, lines[i])
			}
		}
	}
	return strings.Join(kept, "\n")
}
