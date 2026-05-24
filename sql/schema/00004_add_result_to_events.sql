-- +goose Up
ALTER TABLE events ADD COLUMN IF NOT EXISTS result TEXT;

-- +goose Down
ALTER TABLE events DROP COLUMN IF EXISTS result;
