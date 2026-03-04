-- +goose UP
CREATE TABLE event_delivery_logs (
    id SERIAL PRIMARY KEY,
    event_id TEXT NOT NULL,
    status TEXT NOT NULL,        -- processed | retry | failed
    attempt INTEGER NOT NULL,
    error_message TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- +goose DOWN
DROP TABLE event_delivery_logs;