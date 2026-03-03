-- name: InsertEvent :exec
INSERT INTO events (id, type, payload, created_at, processed)
VALUES ($1, $2, $3, $4, $5);
-- name: MarkProcessed :exec
UPDATE events
SET processed = TRUE
WHERE id = $1;
-- name: GetEventByID :one
SELECT id,
  type,
  payload,
  created_at,
  processed
FROM events
WHERE id = $1;
-- name: ListUnprocessedEvents :many
SELECT id,
  type,
  payload,
  created_at,
  processed
FROM events
WHERE processed = FALSE
ORDER BY created_at ASC
LIMIT $1;