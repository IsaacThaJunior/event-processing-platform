-- name: InsertEvent :exec
INSERT INTO events (
    id,
    payload,
    status,
    created_at,
    updated_at,
    type,
    trace_id
  )
VALUES ($1, $2, $3, $4, $5, $6, $7);
-- name: GetEventByID :one
SELECT *
FROM events
WHERE id = $1;
-- name: ListEvents :many
SELECT *
FROM events
ORDER BY created_at ASC;
-- name: UpdateEventStatus :exec
UPDATE events
SET status = $2,
  updated_at = NOW()
WHERE id = $1;