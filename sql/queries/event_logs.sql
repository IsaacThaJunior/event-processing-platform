-- name: InsertDeliveryLog :exec
INSERT INTO event_delivery_logs (
    event_id,
    status,
    attempt,
    error_message
) VALUES (
    $1, $2, $3, $4
);