package main

import (
	"log"
	"net/http"

	"github.com/isaacthajunior/mid-prod/internal/database"
	"github.com/isaacthajunior/mid-prod/internal/repository"
	"github.com/isaacthajunior/mid-prod/internal/worker"
)

func main() {
	pool, err := database.NewPool()
	if err != nil {
		log.Fatalf("failed to connect to db: %v", err)
	}
	defer pool.Close()

	// ✅ Initialize SQLC queries
	queries := database.New(pool)

	// ✅ Create repository
	eventRepo := repository.NewEventRepository(queries)

	// --- We using Redis queue ---
	redisClient := repository.NewRedisClient()
	defer redisClient.Close()

	queue := repository.NewRedisQueue(redisClient, "events_queue")

	// --- Worker pool ---
	workerPool := worker.NewWorkerPool(queue, eventRepo, 3)
	workerPool.Start()
	defer workerPool.Stop()

	log.Println("Connected to Postgres successfully")

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	log.Println("Server running on :8080")
	http.ListenAndServe(":8080", nil)
}
