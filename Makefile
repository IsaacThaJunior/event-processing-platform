include .env
export

# This swaps 'postgres' with 'localhost' only for the Makefile commands
LOCAL_DB_URL=$(subst @postgres:,@localhost:,$(DB_URL))

# Run migrations
migrate-up:
	goose -dir sql/schema postgres "$(LOCAL_DB_URL)" up

migrate-down:
	goose -dir sql/schema postgres "$(LOCAL_DB_URL)" down

migrate-create:
	@read -p "Migration name: " name; \
	goose -dir sql/schema create $$name sql

# Generate sqlc code
sqlc-generate:
	sqlc generate

# Run the app
run:
	air

# Setup database (first time)
# db-create:
# 	createdb mysite

# db-drop:
# 	dropdb mysite --if-exists

# # All-in-one setup
# setup: db-drop db-create migrate-up sqlc-generate

# .PHONY: migrate-up migrate-down migrate-create sqlc-generate run db-create db-drop setup

.PHONY: test test-short test-integration test-coverage test-all

# Run all tests
test:
	go test -v ./...

# Run only short tests (no database/redis containers)
test-short:
	go test -short -v ./...

# Run integration tests
test-integration:
	go test -tags=integration -v ./internal/integration/...

# Run tests with coverage
test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run tests for specific package
test-package:
	go test -v ./internal/$(PACKAGE)/...

# Run tests with race detection
test-race:
	go test -race -v ./...

# Clean test cache
test-clean:
	go clean -testcache

# Run all tests with verbose output
test-verbose:
	go test -v -count=1 ./...

# Run tests with docker compose
test-docker:
	docker-compose -f docker-compose.test.yml up --abort-on-container-exit

# Generate mocks
generate-mocks:
	go generate ./...