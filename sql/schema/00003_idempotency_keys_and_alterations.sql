-- +goose Up
-- Create idempotency keys table
CREATE TABLE idempotency_keys (
    key TEXT PRIMARY KEY,
    event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMP NOT NULL DEFAULT NOW() + INTERVAL '30 days',
    metadata JSONB DEFAULT '{}'::jsonb
);
-- Create indexes for performance
CREATE INDEX idx_idempotency_keys_expires_at ON idempotency_keys(expires_at)
WHERE expires_at IS NOT NULL;
CREATE INDEX idx_idempotency_keys_event_id ON idempotency_keys(event_id);
-- Add idempotency-related columns to events table
ALTER TABLE events
ADD COLUMN IF NOT EXISTS status TEXT DEFAULT 'pending';
ALTER TABLE events
ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT NOW();
ALTER TABLE events
ADD COLUMN IF NOT EXISTS trace_id TEXT NOT NULL;
ALTER TABLE events
ADD COLUMN IF NOT EXISTS priority TEXT;
ALTER TABLE events
ADD COLUMN IF NOT EXISTS parentID TEXT;
ALTER TABLE events
ADD COLUMN IF NOT EXISTS scheduled_at TIMESTAMP;

-- +goose Down
-- Drop indexes
DROP INDEX IF EXISTS idx_idempotency_keys_expires_at;
DROP INDEX IF EXISTS idx_idempotency_keys_event_id;
DROP INDEX IF EXISTS idx_events_whatsapp_message_id;
DROP INDEX IF EXISTS idx_events_status;
DROP INDEX IF EXISTS idx_events_created_at;
-- Drop columns from events table
ALTER TABLE events DROP COLUMN IF EXISTS whatsapp_message_id;
ALTER TABLE events DROP COLUMN IF EXISTS from_number;
ALTER TABLE events DROP COLUMN IF EXISTS command;
ALTER TABLE events DROP COLUMN IF EXISTS status;
ALTER TABLE events DROP COLUMN IF EXISTS updated_at;
-- Drop idempotency keys table
DROP TABLE IF EXISTS idempotency_keys;