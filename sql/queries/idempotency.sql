-- name: CreateIdempotencyKey :execrows
INSERT INTO idempotency_keys (key, event_id, expires_at, metadata)
VALUES ($1, $2, $3, $4) ON CONFLICT (key) DO NOTHING;
-- name: GetIdempotencyKey :one
SELECT *
FROM idempotency_keys
WHERE key = $1
  AND expires_at > NOW();
-- name: DeleteExpiredIdempotencyKeys :execrows
DELETE FROM idempotency_keys
WHERE expires_at < NOW();
-- name: GetIdempotencyKeyStats :one
SELECT COUNT(*) as total_keys,
  COUNT(
    CASE
      WHEN expires_at > NOW() THEN 1
    END
  ) as active_keys,
  COUNT(
    CASE
      WHEN expires_at <= NOW() THEN 1
    END
  ) as expired_keys
FROM idempotency_keys;
-- name: DeleteIdempotencyKey :exec
DELETE FROM idempotency_keys
WHERE key = $1;