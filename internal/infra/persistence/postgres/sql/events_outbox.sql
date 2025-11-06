-- name: EnqueueEvent :one
INSERT INTO events_outbox (
    aggregate_type,
    aggregate_id,
    event_type,
    payload,
    headers,
    available_at
)
VALUES (
    @aggregate_type::text,
    @aggregate_id::text,
    @event_type::text,
    COALESCE(@payload::jsonb, '{}'::jsonb),
    COALESCE(@headers::jsonb, '{}'::jsonb),
    COALESCE(@available_at::timestamptz, NOW())
)
RETURNING *;

-- name: MarkEventDelivered :one
UPDATE events_outbox
SET
    delivered = TRUE,
    published_at = NOW(),
    attempts = attempts + 1
WHERE id = @id::bigint
RETURNING *;

-- name: IncrementEventAttempt :one
UPDATE events_outbox
SET
    attempts = attempts + 1,
    last_error = @last_error::text,
    available_at = NOW() + INTERVAL '30 seconds'
WHERE id = @id::bigint
RETURNING *;

-- name: DequeuePendingEvents :many
SELECT *
FROM events_outbox
WHERE delivered = FALSE
  AND available_at <= NOW()
ORDER BY available_at ASC
LIMIT sqlc.arg('limit')::int;

-- name: DeleteEvent :exec
DELETE FROM events_outbox
WHERE id = @id::bigint;
