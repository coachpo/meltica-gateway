-- name: UpsertStrategyInstance :one
INSERT INTO strategy_instances (
    instance_id,
    strategy_identifier,
    tag,
    status,
    config_hash,
    description,
    metadata,
    updated_at
)
VALUES (
    @instance_id::text,
    @strategy_identifier::text,
    @tag::text,
    @status::text,
    @config_hash::text,
    COALESCE(@description::text, ''),
    COALESCE(@metadata::jsonb, '{}'::jsonb),
    NOW()
)
ON CONFLICT (instance_id) DO
UPDATE SET
    strategy_identifier = EXCLUDED.strategy_identifier,
    tag = EXCLUDED.tag,
    status = EXCLUDED.status,
    config_hash = EXCLUDED.config_hash,
    description = EXCLUDED.description,
    metadata = EXCLUDED.metadata,
    updated_at = NOW()
RETURNING *;

-- name: GetStrategyInternalID :one
SELECT id FROM strategy_instances WHERE instance_id = @instance_id::text;

-- name: DeleteStrategyInstance :exec
DELETE FROM strategy_instances WHERE instance_id = @instance_id::text;

-- name: ListStrategyInstances :many
SELECT *
FROM strategy_instances
ORDER BY instance_id;
