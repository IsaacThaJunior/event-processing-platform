-- name: InsertEvent :exec
INSERT INTO events (
    id,
    payload,
    status,
    created_at,
    updated_at,
    type,
    trace_id,
    priority,
    parentID
  )
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;
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
-- name: CancelEventIfPending :execresult
UPDATE events
SET status = 'cancelled',
  updated_at = NOW()
WHERE id = $1
  AND status = 'pending';
-- name: ListEventsFiltered :many
SELECT id, type, payload, created_at, status, updated_at, trace_id, priority, parentid
FROM events
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('type')::text IS NULL OR type = sqlc.narg('type'))
  AND (sqlc.narg('priority')::text IS NULL OR priority = sqlc.narg('priority'))
  AND (sqlc.narg('search')::text IS NULL OR type ILIKE '%' || sqlc.narg('search') || '%')
ORDER BY created_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');
-- name: CountEventsFiltered :one
SELECT COUNT(*)
FROM events
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('type')::text IS NULL OR type = sqlc.narg('type'))
  AND (sqlc.narg('priority')::text IS NULL OR priority = sqlc.narg('priority'))
  AND (sqlc.narg('search')::text IS NULL OR type ILIKE '%' || sqlc.narg('search') || '%');
-- name: GetEventStatusCounts :many
SELECT COALESCE(status, 'unknown') AS status,
  COUNT(*) AS count
FROM events
GROUP BY status;
-- name: GetRecentProcessedCount :one
SELECT COUNT(*)
FROM events
WHERE status = 'processed'
  AND updated_at >= NOW() - INTERVAL '24 hours';
-- name: ResetTaskForRetry :one
UPDATE events
SET status = 'pending',
  updated_at = NOW()
WHERE id = $1
  AND status IN ('failed', 'cancelled')
RETURNING priority;