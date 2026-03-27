-- name: InsertEvent :exec
INSERT INTO events (
    id,
    whatsapp_message_id,
    from_number,
    command,
    payload,
    status,
    created_at,
    updated_at
  )
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);
-- name: GetEventByID :one
SELECT *
FROM events
WHERE id = $1;
-- name: ListEvents :many
SELECT *
FROM events
ORDER BY created_at ASC;
-- name: GetEventByWhatsappMessageID :one
SELECT *
FROM events
WHERE whatsapp_message_id = $1;
-- name: UpdateEventStatus :exec
UPDATE events
SET status = $2,
  updated_at = NOW()
WHERE id = $1;
