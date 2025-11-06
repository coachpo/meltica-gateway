# Write-Through Cache Semantics

This note clarifies which runtime components keep hot copies of persistence data in memory and how write-through guarantees are preserved.

## Provider Snapshots & Routes

- The provider manager keeps every provider specification and dispatcher route table in-memory inside `manager.states` for fast control-plane reads (`internal/app/provider/manager.go:187`).
- Any `Create`, `Update`, or `Remove` operation **first** updates the in-memory state and then calls the persistence adapter:
  - `persistSnapshot` writes the normalized spec through `providerstore.Store` (`internal/app/provider/manager.go:598`).
  - `persistRoutes` / `deleteRoutes` mirror dispatcher routes via `providerstore.Store.SaveRoutes/DeleteRoutes` (`internal/app/provider/manager.go:669`).
- On bootstrap, `Restore` + `loadRoutes` hydrate the cache from Postgres so dispatcher reconciliation has an authoritative baseline before replaying config (`internal/app/provider/manager.go:623`).
- Reads from the control API (`ProviderMetadataFor`, `Providers`) remain map-backed, so latency stays sub-millisecond while persistence guarantees durability.

## Orders, Executions, Balances, Outbox

- Order lifecycle data is not cached in memory. The order store executes all CRUD straight against Postgres via sqlc bindings (`internal/infra/persistence/postgres/order_store.go:235` et seq.).
- Executions, balances, and outbox entries follow the same pattern—each method validates input, converts to pgx types, and performs a single query/tx. Reads (`List*`) always hit the database to avoid staleness.
- This design keeps caches limited to data that is needed on every control-plane call (providers/routes) and avoids duplicating potentially large order/balance sets in memory.

## Instrumentation

- Provider cache lookups emit `meltica_provider_cache_hits` / `meltica_provider_cache_misses` so dashboards can alert on divergence between runtime lookups and persisted state.
- The pgx pool exposes health gauges (`meltica_db_pool_connections_*`) and migration runs increment `meltica_db_migrations_total`; wire these into Grafana to keep operators aware of DB saturation and schema rollout status.

## Adding New Caches

When introducing another cache layer:

1. **Define ownership** – specify which struct owns the authoritative state and where writes must pass through.
2. **Synchronize writes** – persist first (or atomically) before exposing the updated state to dispatchers/consumers.
3. **Document cache scope** – update this file and the persistence plan whenever a new dataset is cached so operators know what survives process restarts.
4. **Instrument cache health** – expose hit/miss meters and wire them into Grafana alongside existing dispatcher/eventbus dashboards.

Keeping this contract explicit prevents divergence between runtime expectations and the documented persistence strategy.
