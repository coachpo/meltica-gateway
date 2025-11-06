-- name: InsertExecution :one
INSERT INTO executions (
    order_id,
    provider_id,
    execution_id,
    fill_quantity,
    fill_price,
    fee,
    fee_asset,
    liquidity,
    traded_at,
    metadata
)
VALUES (
    @order_id::uuid,
    @provider_id::bigint,
    @execution_id::text,
    @fill_quantity::numeric,
    @fill_price::numeric,
    sqlc.narg('fee')::numeric,
    sqlc.narg('fee_asset')::text,
    @liquidity::text,
    @traded_at::timestamptz,
    COALESCE(sqlc.narg('metadata')::jsonb, '{}'::jsonb)
)
ON CONFLICT (order_id, execution_id) DO UPDATE
SET
    fill_quantity = EXCLUDED.fill_quantity,
    fill_price = EXCLUDED.fill_price,
    fee = EXCLUDED.fee,
    fee_asset = EXCLUDED.fee_asset,
    liquidity = EXCLUDED.liquidity,
    traded_at = EXCLUDED.traded_at,
    metadata = EXCLUDED.metadata
RETURNING *;

-- name: ListExecutionsForOrder :many
SELECT * FROM executions
WHERE order_id = @order_id::uuid
ORDER BY traded_at ASC;

-- name: ListExecutions :many
SELECT
    e.order_id::text AS order_id,
    p.alias AS provider_alias,
    COALESCE(si.instance_id, '') AS strategy_instance_id,
    e.execution_id,
    e.fill_quantity::text AS fill_quantity_text,
    e.fill_price::text AS fill_price_text,
    CASE
        WHEN e.fee IS NULL THEN ''::text
        ELSE e.fee::text
    END AS fee_text,
    e.fee_asset,
    e.liquidity,
    e.traded_at,
    e.metadata AS metadata_json,
    e.created_at
FROM executions e
JOIN orders o ON o.id = e.order_id
JOIN providers p ON p.id = e.provider_id
LEFT JOIN strategy_instances si ON si.id = o.strategy_instance_id
WHERE (
    sqlc.narg('strategy_instance')::text IS NULL
    OR COALESCE(si.instance_id, '') = sqlc.narg('strategy_instance')::text
) AND (
    sqlc.narg('provider_alias')::text IS NULL
    OR p.alias = sqlc.narg('provider_alias')::text
) AND (
    sqlc.narg('order_id')::uuid IS NULL
    OR e.order_id = sqlc.narg('order_id')::uuid
)
ORDER BY e.traded_at DESC
LIMIT sqlc.arg('limit')::int;
