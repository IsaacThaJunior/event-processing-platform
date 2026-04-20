-- name: UpsertDeliveryLog :exec
INSERT INTO event_delivery_logs (
    event_id,
    status,
    attempt,
    error_message,
    created_at,
    updated_at
)
VALUES (
    $1, $2, $3, $4, NOW(), NOW()
)
ON CONFLICT (event_id)
DO UPDATE SET
    status = EXCLUDED.status,
    attempt = EXCLUDED.attempt,
    error_message = EXCLUDED.error_message,
    updated_at = NOW();

-- name: GetDeliveryLogsForEvent :many
SELECT *
FROM event_delivery_logs
WHERE event_id = $1
ORDER BY attempt ASC;