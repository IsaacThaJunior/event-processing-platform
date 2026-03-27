package worker

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func SetupTestDB(t *testing.T) (*pgxpool.Pool, func()) {
	if testing.Short() {
		t.Skip("Skipping database test in short mode")
	}

	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "postgres:15-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "testuser",
			"POSTGRES_PASSWORD": "testpass",
			"POSTGRES_DB":       "testdb",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections"),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start postgres container: %v", err)
	}

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("Failed to get port: %v", err)
	}

	dsn := fmt.Sprintf("postgres://testuser:testpass@localhost:%s/testdb?sslmode=disable", port.Port())

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}

	// Wait for database to be ready
	time.Sleep(2 * time.Second)

	// Run migrations
	sqlDB := stdlib.OpenDB(*config.ConnConfig)
	defer sqlDB.Close()

	// Try to find the migrations directory
	migrationPaths := []string{
		"../../sql/schema",
		"../../migrations",
		"../migrations",
		"migrations",
		"../../../sql/schema",
	}

	var migrationPath string
	for _, path := range migrationPaths {
		if _, err := os.Stat(path); err == nil {
			migrationPath = path
			break
		}
	}

	if migrationPath != "" {
		t.Logf("Running migrations from: %s", migrationPath)
		if err := goose.Up(sqlDB, migrationPath); err != nil {
			t.Fatalf("Failed to run migrations from %s: %v", migrationPath, err)
		}
	} else {
		t.Log("No migrations found, creating schema manually")
		if err := createSchemaManually(ctx, pool); err != nil {
			t.Fatalf("Failed to create schema manually: %v", err)
		}
	}

	cleanup := func() {
		pool.Close()
		container.Terminate(ctx)
	}

	return pool, cleanup
}

// SetupTestQueries creates a sqlc Queries instance with a test database
func SetupTestQueries(t *testing.T) (*pgxpool.Pool, func()) {
	return SetupTestDB(t)
}

func createSchemaManually(ctx context.Context, pool *pgxpool.Pool) error {
	schema := `
    -- events table
    CREATE TABLE IF NOT EXISTS events (
        id TEXT PRIMARY KEY,
        type TEXT NOT NULL,
        payload TEXT NOT NULL,
        created_at TIMESTAMP NOT NULL,
        processed BOOLEAN DEFAULT FALSE,
        whatsapp_message_id TEXT UNIQUE,
        from_number TEXT,
        command TEXT,
        status TEXT DEFAULT 'pending',
        updated_at TIMESTAMP DEFAULT NOW()
    );

    -- event_delivery_logs table
    CREATE TABLE IF NOT EXISTS event_delivery_logs (
        id SERIAL PRIMARY KEY,
        event_id TEXT NOT NULL,
        status TEXT NOT NULL,
        attempt INTEGER NOT NULL,
        error_message TEXT,
        created_at TIMESTAMP NOT NULL DEFAULT NOW()
    );

    -- idempotency_keys table
    CREATE TABLE IF NOT EXISTS idempotency_keys (
        key TEXT PRIMARY KEY,
        event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
        created_at TIMESTAMP NOT NULL DEFAULT NOW(),
        expires_at TIMESTAMP NOT NULL DEFAULT NOW() + INTERVAL '30 days',
        metadata JSONB DEFAULT '{}'::jsonb
    );

    -- indexes
    CREATE INDEX IF NOT EXISTS idx_idempotency_keys_expires_at ON idempotency_keys(expires_at);
    CREATE INDEX IF NOT EXISTS idx_events_whatsapp_message_id ON events(whatsapp_message_id);
    CREATE INDEX IF NOT EXISTS idx_events_status ON events(status);
    `

	_, err := pool.Exec(ctx, schema)
	return err
}
