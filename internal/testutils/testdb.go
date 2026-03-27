// internal/testutil/testdb.go
package testutil

import (
	"context"
	"fmt"
	"testing"

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

	// Connection string for pgx
	dsn := fmt.Sprintf("postgres://testuser:testpass@localhost:%s/testdb?sslmode=disable", port.Port())

	// Create pgxpool connection
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}

	// Test connection
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}

	// Run migrations using goose with stdlib driver
	sqlDB := stdlib.OpenDB(*config.ConnConfig)
	defer sqlDB.Close()

	// Use the sql/schema directory for migrations
	// Path from internal/testutil/ to project root: ../../sql/schema
	migrationPath := "../../sql/schema"

	t.Logf("Running migrations from: %s", migrationPath)

	if err := goose.Up(sqlDB, migrationPath); err != nil {
		t.Fatalf("Failed to run migrations from %s: %v", migrationPath, err)
	}

	t.Log("Migrations completed successfully")

	cleanup := func() {
		pool.Close()
		container.Terminate(ctx)
	}

	return pool, cleanup
}
