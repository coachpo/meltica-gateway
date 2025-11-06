-- Enable extensions required by the persistence layer.
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE providers (
    id BIGSERIAL PRIMARY KEY,
    alias TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    adapter_identifier TEXT NOT NULL,
    connection JSONB NOT NULL DEFAULT '{}'::JSONB,
    status TEXT NOT NULL DEFAULT 'inactive',
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX providers_adapter_identifier_idx ON providers (adapter_identifier);

CREATE TABLE strategy_instances (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    strategy_identifier TEXT NOT NULL,
    version TEXT NOT NULL,
    status TEXT NOT NULL,
    config_hash TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX strategy_instances_identifier_version_idx
    ON strategy_instances (strategy_identifier, version);

CREATE TABLE provider_routes (
    id BIGSERIAL PRIMARY KEY,
    provider_id BIGINT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
    symbol TEXT NOT NULL,
    route JSONB NOT NULL,
    version INT NOT NULL DEFAULT 1,
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider_id, symbol)
);

CREATE TABLE orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id BIGINT NOT NULL REFERENCES providers(id) ON DELETE RESTRICT,
    strategy_instance_id UUID REFERENCES strategy_instances(id) ON DELETE SET NULL,
    client_order_id TEXT NOT NULL,
    instrument TEXT NOT NULL,
    side TEXT NOT NULL,
    order_type TEXT NOT NULL,
    quantity NUMERIC(32, 16) NOT NULL,
    price NUMERIC(32, 16),
    state TEXT NOT NULL,
    external_order_ref TEXT,
    placed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    acknowledged_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider_id, client_order_id)
);

CREATE INDEX orders_provider_state_idx ON orders (provider_id, state);
CREATE INDEX orders_strategy_instance_idx ON orders (strategy_instance_id);

CREATE TABLE executions (
    id BIGSERIAL PRIMARY KEY,
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    provider_id BIGINT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
    execution_id TEXT NOT NULL,
    fill_quantity NUMERIC(32, 16) NOT NULL,
    fill_price NUMERIC(32, 16) NOT NULL,
    fee NUMERIC(32, 16),
    fee_asset TEXT,
    liquidity TEXT,
    traded_at TIMESTAMPTZ NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (order_id, execution_id)
);

CREATE INDEX executions_provider_idx ON executions (provider_id);

CREATE TABLE balances (
    id BIGSERIAL PRIMARY KEY,
    provider_id BIGINT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
    asset TEXT NOT NULL,
    total NUMERIC(32, 16) NOT NULL,
    available NUMERIC(32, 16) NOT NULL,
    snapshot_at TIMESTAMPTZ NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider_id, asset, snapshot_at)
);

CREATE INDEX balances_provider_asset_idx ON balances (provider_id, asset);

CREATE TABLE events_outbox (
    id BIGSERIAL PRIMARY KEY,
    aggregate_type TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    headers JSONB NOT NULL DEFAULT '{}'::JSONB,
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ,
    attempts INT NOT NULL DEFAULT 0,
    last_error TEXT,
    delivered BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX events_outbox_dispatch_idx
    ON events_outbox (delivered, available_at);

