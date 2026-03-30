package main

import (
	"fmt"
	"log"
	"log/slog"
	"net/http"

	"github.com/isaacthajunior/mid-prod/internal/database"
	"github.com/isaacthajunior/mid-prod/internal/handler"
	"github.com/isaacthajunior/mid-prod/internal/metrics"
	"github.com/isaacthajunior/mid-prod/internal/repository"
	"github.com/isaacthajunior/mid-prod/internal/service"
	"github.com/isaacthajunior/mid-prod/internal/worker"
)

func main() {
	logger := slog.Default()
	pool, err := database.NewPool()
	if err != nil {
		log.Fatalf("failed to connect to db: %v", err)
	}
	log.Println("Connected to Postgres successfully")

	defer pool.Close()

	// ✅ Initialize SQLC queries
	queries := database.New(pool)

	// ✅ Create repository
	eventRepo := repository.NewEventRepository(queries)

	// Create the idempotency service
	idempotencyService := service.NewIdempotencyService(queries, pool)

	// Create the command parser
	commandParser := service.NewCommandParser()

	// --- We using Redis queue ---
	redisClient := repository.NewRedisClient()
	defer redisClient.Close()

	queue := repository.NewRedisQueue(redisClient, "events_queue")

	whatsAppSvc := handler.NewWhatsAppHandler(queue, commandParser, logger, eventRepo, idempotencyService)

	// --- Worker pool ---
	workerPool := worker.NewWorkerPool(queue, eventRepo, 3, logger)
	workerPool.Start()
	defer workerPool.Stop()

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		depth, _ := queue.Depth()

		fmt.Fprintf(w, `
total_processed: %d
total_failed: %d
total_retried: %d
queue_depth: %d
`,
			metrics.TotalProcessed,
			metrics.TotalFailed,
			metrics.TotalRetried,
			depth,
		)
	})

	// ✅ WhatsApp webhook endpoint (main webhook)
	http.HandleFunc("/webhook", whatsAppSvc.HandleWebhook)

	// ✅ WhatsApp verification endpoint (for Meta webhook setup)
	http.HandleFunc("/webhook/verify", whatsAppSvc.HandleVerification)

	// // ✅ Test endpoint to push events (for manual testing)
	http.HandleFunc("/test/push", whatsAppSvc.HandleTestPush)

	log.Println("Server running on :8080")
	log.Println("Endpoints:")
	log.Println("  GET  /health           - Health check")
	log.Println("  GET  /metrics          - Metrics")
	log.Println("  POST /webhook          - WhatsApp webhook")
	log.Println("  GET  /webhook/verify   - WhatsApp verification")
	log.Println("  POST /test/push        - Manual test push")
	http.ListenAndServe(":8080", nil)
}
