-- name: UpsertBalanceSnapshot :one
INSERT INTO balances (
    provider_id,
    asset,
    total,
    available,
    snapshot_at,
    metadata
)
VALUES (
    @provider_id::bigint,
    @asset::text,
    @total::numeric,
    @available::numeric,
    @snapshot_at::timestamptz,
    COALESCE(@metadata::jsonb, '{}'::jsonb)
)
ON CONFLICT (provider_id, asset, snapshot_at) DO UPDATE
SET
    total = EXCLUDED.total,
    available = EXCLUDED.available,
    metadata = EXCLUDED.metadata,
    updated_at = NOW()
RETURNING *;

-- name: LatestBalanceForAsset :one
SELECT *
FROM balances
WHERE provider_id = @provider_id::bigint AND asset = @asset::text
ORDER BY snapshot_at DESC
LIMIT 1;

-- name: ListBalancesSince :many
SELECT *
FROM balances
WHERE provider_id = @provider_id::bigint
  AND snapshot_at >= @since::timestamptz
ORDER BY snapshot_at DESC;

-- name: ListBalances :many
SELECT
    p.alias AS provider_alias,
    b.asset,
    b.total::text AS total_text,
    b.available::text AS available_text,
    b.snapshot_at,
    b.metadata AS metadata_json,
    b.created_at,
    b.updated_at
FROM balances b
JOIN providers p ON p.id = b.provider_id
WHERE (
    sqlc.narg('provider_alias')::text IS NULL
    OR p.alias = sqlc.narg('provider_alias')::text
) AND (
    sqlc.narg('asset')::text IS NULL
    OR b.asset = sqlc.narg('asset')::text
)
ORDER BY b.snapshot_at DESC
LIMIT sqlc.arg('limit')::int;
