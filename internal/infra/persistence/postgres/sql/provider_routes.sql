-- name: UpsertProviderRoute :one
INSERT INTO provider_routes (
    provider_id,
    symbol,
    route,
    version,
    metadata,
    updated_at
)
VALUES (
    @provider_id::bigint,
    @symbol::text,
    COALESCE(@route::jsonb, '{}'::jsonb),
    COALESCE(@version::int, 1),
    COALESCE(@metadata::jsonb, '{}'::jsonb),
    NOW()
)
ON CONFLICT (provider_id, symbol) DO
UPDATE SET
    route = EXCLUDED.route,
    version = EXCLUDED.version,
    metadata = EXCLUDED.metadata,
    updated_at = NOW()
RETURNING *;

-- name: DeleteProviderRoute :exec
DELETE FROM provider_routes
WHERE provider_id = @provider_id::bigint AND symbol = @symbol::text;

-- name: DeleteRoutesByProvider :exec
DELETE FROM provider_routes
WHERE provider_id = @provider_id::bigint;

-- name: ListProviderRoutes :many
SELECT * FROM provider_routes
WHERE provider_id = @provider_id::bigint
ORDER BY symbol;
