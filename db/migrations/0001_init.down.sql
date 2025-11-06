DROP INDEX IF EXISTS events_outbox_dispatch_idx;
DROP TABLE IF EXISTS events_outbox;

DROP INDEX IF EXISTS balances_provider_asset_idx;
DROP TABLE IF EXISTS balances;

DROP INDEX IF EXISTS executions_provider_idx;
DROP TABLE IF EXISTS executions;

DROP INDEX IF EXISTS orders_strategy_instance_idx;
DROP INDEX IF EXISTS orders_provider_state_idx;
DROP TABLE IF EXISTS orders;

DROP TABLE IF EXISTS provider_routes;

DROP INDEX IF EXISTS strategy_instances_identifier_version_idx;
DROP TABLE IF EXISTS strategy_instances;

DROP INDEX IF EXISTS providers_adapter_identifier_idx;
DROP TABLE IF EXISTS providers;

DROP EXTENSION IF EXISTS "pgcrypto";
