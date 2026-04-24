package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/isaacthajunior/mid-prod/internal/database"
	"github.com/isaacthajunior/mid-prod/internal/handler"
	"github.com/isaacthajunior/mid-prod/internal/metrics"
	"github.com/isaacthajunior/mid-prod/internal/repository"
	"github.com/isaacthajunior/mid-prod/internal/service"
	"github.com/isaacthajunior/mid-prod/internal/taskerr"
	"github.com/isaacthajunior/mid-prod/internal/worker"

	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
	pkgerr "github.com/pkg/errors"
	"gopkg.in/natefinch/lumberjack.v2"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	httpPort := 8080
	status := run(ctx, cancel, httpPort)
	cancel()
	os.Exit(status)
}

func run(ctx context.Context, cancel context.CancelFunc, port int) int {
	logger, closeFunc, err := initializeLogger()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		return 1
	}

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
	taskHandler := handler.NewTaskHanler(queue, eventRepo, idempotencyService, validator)

	// Admin handler
	adminRepo := repository.NewAdminRepository(queries)
	adminHandler := handler.NewAdminHandler(adminRepo, queue, workerPool)

	s := newServer(adminHandler, taskHandler, cancel, logger, port)
	var serverErr error
	go func() {
		serverErr = s.start()
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.shutdown(shutdownCtx); err != nil {
		logger.Error("An error occured", "message", "failed to shutdown server", "err", err)
		return 1
	}
	if serverErr != nil {
		logger.Error("An error occured", "message", "server error", "err", serverErr)
		return 1
	}

	defer func() {
		if err := closeFunc(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing log file: %v\n", err)
		}
	}()
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
	logFile := "logs/tasks.json"

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
		attrs = append(attrs, slog.String("stack_trace", fmt.Sprintf("%+v", stackErr.StackTrace())))
	}

	return attrs
}
