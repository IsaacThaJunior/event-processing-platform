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