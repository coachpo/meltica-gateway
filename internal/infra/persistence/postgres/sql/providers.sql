-- name: UpsertProvider :one
INSERT INTO providers (
    alias,
    display_name,
    adapter_identifier,
    connection,
    status,
    metadata,
    updated_at
)
VALUES (
    @alias::text,
    @display_name::text,
    @adapter_identifier::text,
    COALESCE(@connection::jsonb, '{}'::jsonb),
    @status::text,
    COALESCE(@metadata::jsonb, '{}'::jsonb),
    NOW()
)
ON CONFLICT (alias) DO
UPDATE SET
    display_name = EXCLUDED.display_name,
    adapter_identifier = EXCLUDED.adapter_identifier,
    connection = EXCLUDED.connection,
    status = EXCLUDED.status,
    metadata = EXCLUDED.metadata,
    updated_at = NOW()
RETURNING *;

-- name: GetProviderByAlias :one
SELECT * FROM providers WHERE alias = @alias::text;

-- name: DeleteProviderByAlias :exec
DELETE FROM providers WHERE alias = @alias::text;

-- name: ListProviders :many
SELECT * FROM providers ORDER BY alias;

-- name: GetProviderID :one
SELECT id FROM providers WHERE alias = @alias::text;
