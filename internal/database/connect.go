package database

import (
	"context"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool creates a pgxpool sized to the given worker count.
// MaxConns = workers * 3 covers worker DB activity plus HTTP handler overhead.
func NewPool(workerCount int) (*pgxpool.Pool, error) {
	dbURL := os.Getenv("DB_URL")
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = int32(workerCount * 3)
	cfg.MinConns = int32(workerCount)
	return pgxpool.NewWithConfig(context.Background(), cfg)
}
