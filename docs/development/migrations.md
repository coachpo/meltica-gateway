# Database Migrations

Meltica uses [`golang-migrate`](https://github.com/golang-migrate/migrate) to manage schema changes under `db/migrations/`.

## Prerequisites

- Install the upstream CLI (`brew install golang-migrate`) only if you plan to run `migrate create ...` for new files. Applying migrations via `make migrate` now uses the repository's built-in runner (`go run ./cmd/migrate`), so no external binary is required.
- Ensure PostgreSQL is running locally and accessible via the DSN `postgresql://localhost:5432/meltica`.

## Commands

| Command          | Description                                                         |
| ---------------- | ------------------------------------------------------------------- |
| `make migrate`   | Apply all pending migrations to the database specified by `DATABASE_URL` using the first-party runner. |
| `make migrate-down` | Roll back the most recent migration via the same runner.                                   |
| `make sqlc`      | Regenerate typed query bindings from SQL files.                      |

The Makefile defaults `DATABASE_URL` to `postgresql://localhost:5432/meltica?sslmode=disable`. Override it by exporting the variable before invoking `make`:

```bash
export DATABASE_URL="postgresql://postgres:root@localhost:5432/meltica?sslmode=disable"
make migrate
```

### Gateway-assisted migrations

- The compiled `gateway` binary automatically calls `runDatabaseMigrations` on startup whenever `database.runMigrations` is `true` in the loaded config (`config/app.yaml` and variants).
- Those migrations are **not embedded** in the binary. The SQL files under `db/migrations/` (or the directory pointed to by `-path`) must be available on disk next to the binary so the bootstrap step can load them.
- If you only have the gateway executable, you can still migrate by starting it once with the desired config (it exits or continues running afterward), or by disabling the flag and relying on the `cmd/migrate` CLI for manual control.

## Creating New Migrations

Generate new migration files with the CLI once `golang-migrate` is installed:

```bash
migrate create -ext sql -dir db/migrations -seq add_orders_table
```

The `-seq` flag ensures filenames include an incrementing sequence number compatible with the Makefile targets.

### CI Reminders

- Run `make migrate` against a disposable database during CI to validate new migrations.
- Follow with `make migrate-down` to ensure the down scripts still succeed.
- Always rerun `make sqlc` after editing SQL so generated bindings stay in sync.
- Surface `meltica_db_migrations_total` and `meltica_db_pool_connections_*` in dashboards to catch drift early.

## Generating Query Bindings

Run `make sqlc` (a thin wrapper around `sqlc generate`) whenever SQL files under `internal/infra/persistence/postgres/sql/` change. The configuration in `sqlc.yaml` emits Go bindings into `internal/infra/persistence/postgres/sqlc/`.

## Resetting the Development Database

Use the helper script to drop and recreate the `meltica` database locally:

```bash
./scripts/db/dev_reset.sh
```

Override the admin connection by exporting `DATABASE_URL` (defaults to `postgresql://postgres:root@localhost:5432/postgres?sslmode=disable`) or change the target database with `TARGET_DB`.

## Operational Runbook

> Applies to staging/production environments where PostgreSQL is authoritative.

1. **Pre-flight**
   - Take a snapshot or point-in-time backup of the target database.
   - Verify replicas/standbys are healthy. If using streaming replication, confirm WAL apply delay is <1s.
   - Ensure alerts for `meltica_db_pool_connections_acquired` and `meltica_db_migrations_total` are visible in Grafana.

2. **Apply migrations**
   ```bash
   export DATABASE_URL="postgres://user:pass@db-host:5432/meltica?sslmode=require"
   make migrate
   ```
   - Watch `meltica_db_migrations_total{result="applied"}` to confirm the run.
   - Check application logs for `database migrations applied successfully`.

3. **Roll back (if required)**
   ```bash
   make migrate-down
   ```
   - Only run once per failure; re-run `make migrate` after fixing the issue.
   - Always restore from backup if data was mutated by partial migrations.

4. **Vacuum / Analyze**
   - For large table rewrites, schedule `VACUUM (ANALYZE)` outside peak hours.
   - Monitor `pg_stat_all_tables.n_dead_tup` and reindex if bloat exceeds operational thresholds.

5. **Failover checklist**
   - Re-run `make migrate` against the promoted primary to ensure schema parity.
   - Validate application connectivity and `meltica_db_pool_connections_*` gauges on the new primary.
   - Update secrets/config to point at the new DSN.

6. **Post-run verification**
   - Run `make test` (or targeted persistence suites) against a staging environment.
   - Confirm `meltica_provider_cache_hits` continues to grow; sustained misses may indicate cache desync after the rollout.
