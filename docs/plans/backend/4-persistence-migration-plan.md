# Meltica Persistence Migration Plan

## Goals and Non-Goals

- Provide durable storage for provider state, order lifecycle, executions, balances, and strategy metadata using PostgreSQL.
- Preserve modular layering (`internal/app`, `internal/domain`, `internal/infra`, `internal/support`) and keep runtime components testable via dependency injection.
- Enable horizontal scalability by moving shared state out of process while allowing read caches to remain in memory for latency.
- Retire the legacy in-memory persistence paths; gateway startups must have PostgreSQL connectivity.
- Maintain configuration-as-code: database connection details live in `config/app.yaml` (with env overrides) and are never sourced from the database itself.
- Out of scope for this iteration: replacing the in-memory event bus, introducing additional message brokers, or redesigning the HTTP control API.

## Current State Assessment

- Runtime emphasises transient state (`internal/README.md:35-39`), keeping provider routes, order tracking, and risk metrics in memory.
- Minimal scope planning explicitly deferred durable storage (`docs/analysis/MINIMAL_SCOPE.md:6-16`).
- Configuration lacks database connectivity settings (`config/app.yaml:1-30` and `internal/infra/config/app_config.go:17-160`).
- Event bus (`internal/infra/bus/eventbus/memory.go:22-118`) and provider manager (`internal/app/provider/manager.go:22-99`) rely on atomics, maps, and pools that reset on restart.
- Strategy assets already persist to disk through `internal/app/lambda/js/loader.go:715-789`, providing a precedent for file-backed durability.

## Migration Environment

- All upgrade runs target the DSN `postgresql://localhost:5432/meltica` using `user=postgres` and `password=root`, and migrations connect through the `pgx` driver to stay aligned with runtime dependencies.

## Target Architecture

### Persistence Layer Modules

- Introduce `internal/infra/persistence` with driver-specific subpackages (initially `postgres`).
- Use `pgxpool` for connection pooling; expose an interface `Store` with methods grouped by bounded context (providers, orders, executions, balances, telemetry).
- Generate SQL accessors with `sqlc` to produce typed repositories while keeping queries in `internal/infra/persistence/postgres/sql/`.
- Configure migrations under `db/migrations/` managed via `golang-migrate` (invoked from `make migrate`).
- Provide lightweight testing harnesses (e.g., ephemeral Postgres instances) to support contract and unit testing.

### Runtime Integration

- Extend `config.AppConfig` with `DatabaseConfig` containing DSN, max connections, timeouts, and migration flags.
- Instantiate a PostgreSQL store in `cmd/gateway/main.go`; fail fast during bootstrap if connectivity or migrations are unavailable.
- Refactor provider manager and lambdas to depend on repository interfaces, injected through constructors instead of owning maps directly.
- Implement a write-through cache strategy: critical reads hit in-memory maps, while writes synchronously persist to PostgreSQL within transactions.
- Introduce an outbox table for event bus publishing to allow replay and ensure at-least-once delivery for critical events without removing the in-memory bus.

## PostgreSQL Schema Blueprint

- `providers` – provider metadata, connection status, credentials references, lifecycle audit columns.
- `provider_routes` – flattened dispatcher routes keyed by provider and symbol, supporting reconciliation and restart recovery.
- `orders` – canonical orders with UUID, client order id, instrument, side, quantity, price, state, and timestamps.
- `executions` – order fills linked to `orders`, including execution id, fill quantity, price, fees, and exchange references.
- `balances` – provider and asset balances with snapshot timestamps.
- `strategy_instances` – active strategy assignments, configuration hashes, status, runtime metadata.
- `events_outbox` – pending domain events (JSON payload plus metadata) to bridge to downstream systems or rehydrate the bus.
- Apply naming and indexing conventions (snake_case columns, bigint IDs, `created_at`/`updated_at`, `deleted_at` nullable for soft deletes).

## Implementation Phases

- Migration upgrades will target PostgreSQL via the DSN `postgresql://localhost:5432/meltica` using credentials `user=postgres` and `password=root`, executed through the `pgx` driver to stay aligned with runtime expectations.

1. **Foundations**
   - Add `DatabaseConfig` to config structs and YAML, keeping all connection metadata in configuration files or environment variables rather than in PostgreSQL.
   - Create migration scaffolding (`db/migrations`) and wire `Makefile` targets (`make migrate`, `make migrate-down`).
   - Stand up persistence bootstrap in `cmd/gateway/main.go`, including health checks and graceful shutdown hooks.

2. **Schema and Repositories**
   - Define initial migrations covering providers, orders, executions, balances, strategies, and outbox.
   - Generate repository code with `sqlc`; expose interfaces in `internal/domain` or dedicated `internal/app` adapters.
   - Add integration tests using Docker-based Postgres (Testcontainers) under `tests/contract/persistence`.
   - Introduce domain-level repositories for orders, executions, and balances to decouple `core` from Postgres specifics.

3. **Provider and Strategy State Migration**
   - Replace in-memory provider registry storage with persistence-backed repositories.
   - Snapshot dispatcher routes to `provider_routes` and reload them during start-up to restore state.
   - Persist cached dispatcher routes on shutdown and prune entries when providers are removed so the database reflects active topology.
   - Persist strategy instance metadata (`lambda` runtime) to `strategy_instances`, ensuring refresh and deletion flows write to both disk and DB.
   - Rehydrate provider specifications from persisted snapshots during gateway bootstrap before reconciling config-managed providers.

4. **Order Lifecycle Persistence**
   - Refactor `BaseLambda` to write orders and state transitions into `orders` within transactions when calling submit/ack handlers.
   - Record execution reports and balance updates to `executions` and `balances`, respectively, ensuring idempotency by keying on exchange IDs.
   - Provide read APIs for historical queries through the HTTP control plane.

### Write-Through Cache Status (Updated 2025-02-14)

- Provider specs and dispatcher routes are cached in-memory by `internal/app/provider/manager.go` and synchronously written through to Postgres via `providerstore.Store` (`persistSnapshot` / `persistRoutes`). On bootstrap the cache is rehydrated from persistence before providers start.
- Orders, executions, balances, and outbox flows rely entirely on database reads/writes via sqlc repositories—no additional caching exists today.
- Future cache introductions must update `docs/development/write-through-cache.md` so operators know which datasets survive restarts and which require database queries.

5. **Event Outbox and Replay**
   - Persist published events to `events_outbox` before dispatch, mark them delivered after successful fan-out.
  - Introduce a background worker to replay undelivered events on startup or after failures.
   - Expose administrative endpoints to query and purge the outbox.

6. **Rollout and Hardening**
   - Mandate PostgreSQL across all environments and remove the memory driver from runtime builds.
   - Run performance benchmarks, tune indexes, and validate connection pooling. (Dashboards now include `meltica_db_pool_connections_*` and `meltica_db_migrations_total`.)
   - Update operational docs, runbooks, and dashboards to monitor DB metrics and replica lag (if applicable). (See `docs/development/migrations.md` for the runbook.)
   - Remove reliance on `lambdaManifest` YAML; bootstrap instances from the database/control plane only.

## Testing and Quality Strategy

- Extend `make test` pipeline to spin up ephemeral Postgres for both integration and unit-level persistence tests.
- Add contract tests ensuring persistence APIs conform to expected behavior (upserts, optimistic locking, event replay).
- Update architecture tests to enforce layer boundaries around the new `internal/infra/persistence` package.
- Integrate `golangci-lint` checks for SQL files (via `sqlc vet`) and ensure migrations are idempotent by applying them twice in CI.

## Operational Considerations

- Provision managed Postgres (staging and prod) with automated backups, point-in-time recovery, encryption at rest, and TLS in transit.
- Implement migration safety: take schema backups, run migrations under feature flags, and include rollback scripts.
- Add telemetry spans and metrics for database interactions (latency histograms, error counters) using existing `telemetry` infrastructure.
- Document runbooks for schema changes, failover, and vacuum/analyze procedures.

## Risks and Mitigations

- **Data consistency**: enforce transactions for multi-step mutations; add unique constraints and optimistic locking columns.
- **Performance regressions**: cache hot data in memory, benchmark and index critical queries; consider read replicas if needed.
- **Operational complexity**: invest in tooling (`make migrate`, status commands) and dashboards before enabling Postgres in production.
- **Testing overhead**: streamline containerized DB startup and provide fixtures to keep developer workflows fast.

## Follow-Up Backlog

- Evaluate replacing or augmenting the in-memory event bus with a durable message broker once Postgres persistence stabilizes.
- Design analytics pipelines (PnL, compliance) leveraging the persisted data.
- Expand auditing with append-only ledgers and immutable history for regulated environments.
- Update frontend tooling to manage lambda instances via the persisted API (see backend-plan.md upgrade guide).
