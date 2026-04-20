-- +goose UP
CREATE TABLE event_delivery_logs (
    event_id TEXT PRIMARY KEY,
    status TEXT NOT NULL,
    attempt INTEGER NOT NULL DEFAULT 0,
    error_message TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- +goose DOWN
DROP TABLE event_delivery_logs;