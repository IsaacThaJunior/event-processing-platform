package database

import (
	"context"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool() (*pgxpool.Pool, error) {
	dbURL := os.Getenv("DB_URL")
	return pgxpool.New(context.Background(), dbURL)
}
