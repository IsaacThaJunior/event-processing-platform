-- name: InsertDeliveryLog :exec
INSERT INTO event_delivery_logs (
    event_id,
    status,
    attempt,
    error_message
) VALUES (
    $1, $2, $3, $4
);

-- name: GetDeliveryLogsForEvent :many
SELECT *
FROM event_delivery_logs
WHERE event_id = $1
ORDER BY attempt ASC;