-- name: InsertOrder :one
INSERT INTO orders (
    id,
    provider_id,
    strategy_instance_id,
    client_order_id,
    instrument,
    side,
    order_type,
    quantity,
    price,
    state,
    external_order_ref,
    placed_at,
    acknowledged_at,
    completed_at,
    metadata
)
VALUES (
    COALESCE(@id::uuid, gen_random_uuid()),
    @provider_id::bigint,
    sqlc.narg('strategy_instance_id')::uuid,
    @client_order_id::text,
    @instrument::text,
    @side::text,
    @order_type::text,
    @quantity::numeric,
    sqlc.narg('price')::numeric,
    @state::text,
    sqlc.narg('external_order_ref')::text,
    COALESCE(sqlc.narg('placed_at')::timestamptz, NOW()),
    sqlc.narg('acknowledged_at')::timestamptz,
    sqlc.narg('completed_at')::timestamptz,
    COALESCE(sqlc.narg('metadata')::jsonb, '{}'::jsonb)
)
ON CONFLICT (id) DO NOTHING
RETURNING *;

-- name: UpdateOrderState :one
UPDATE orders
SET
    state = @state::text,
    acknowledged_at = COALESCE(sqlc.narg('acknowledged_at')::timestamptz, acknowledged_at),
    completed_at = COALESCE(sqlc.narg('completed_at')::timestamptz, completed_at),
    metadata = COALESCE(sqlc.narg('metadata')::jsonb, metadata),
    updated_at = NOW()
WHERE id = @id::uuid
RETURNING *;

-- name: GetOrderByClientID :one
SELECT * FROM orders
WHERE provider_id = @provider_id::bigint AND client_order_id = @client_order_id::text;

-- name: ListOrdersByProvider :many
SELECT * FROM orders
WHERE provider_id = @provider_id::bigint
ORDER BY placed_at DESC
LIMIT sqlc.arg('limit')::int OFFSET sqlc.arg('offset')::int;

-- name: ListOrders :many
SELECT
    o.id::text AS order_id,
    p.alias AS provider_alias,
    COALESCE(si.instance_id, '') AS strategy_instance_id,
    o.client_order_id,
    o.instrument,
    o.side,
    o.order_type,
    o.quantity::text AS quantity_text,
    CASE
        WHEN o.price IS NULL THEN ''::text
        ELSE o.price::text
    END AS price_text,
    o.state,
    o.external_order_ref,
    o.placed_at,
    o.acknowledged_at,
    o.completed_at,
    o.metadata AS metadata_json,
    o.created_at,
    o.updated_at
FROM orders o
JOIN providers p ON p.id = o.provider_id
LEFT JOIN strategy_instances si ON si.id = o.strategy_instance_id
WHERE (
    sqlc.narg('strategy_instance')::text IS NULL
    OR COALESCE(si.instance_id, '') = sqlc.narg('strategy_instance')::text
) AND (
    sqlc.narg('provider_alias')::text IS NULL
    OR p.alias = sqlc.narg('provider_alias')::text
) AND (
    sqlc.narg('states')::text[] IS NULL
    OR o.state = ANY(sqlc.narg('states')::text[])
)
ORDER BY o.placed_at DESC
LIMIT sqlc.arg('limit')::int;
