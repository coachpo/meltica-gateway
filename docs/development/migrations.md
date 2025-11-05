# Database Migrations

Meltica uses [`golang-migrate`](https://github.com/golang-migrate/migrate) to manage schema changes under `db/migrations/`.

## Prerequisites

- Install the CLI: `brew install golang-migrate` or download binaries from the upstream releases page.
- Ensure PostgreSQL is running locally and accessible via the DSN `postgresql://localhost:5432/meltica`.

## Commands

| Command          | Description                                                         |
| ---------------- | ------------------------------------------------------------------- |
| `make migrate`   | Apply all pending migrations to the database specified by `DATABASE_URL`. |
| `make migrate-down` | Roll back the most recent migration.                                   |

The Makefile defaults `DATABASE_URL` to `postgresql://localhost:5432/meltica?sslmode=disable`. Override it by exporting the variable before invoking `make`:

```bash
export DATABASE_URL="postgresql://postgres:root@localhost:5432/meltica?sslmode=disable"
make migrate
```

## Creating New Migrations

Generate new migration files with the CLI once `golang-migrate` is installed:

```bash
migrate create -ext sql -dir db/migrations -seq add_orders_table
```

The `-seq` flag ensures filenames include an incrementing sequence number compatible with the Makefile targets.

## Generating Query Bindings

Run `sqlc generate` whenever SQL files under `internal/infra/persistence/postgres/sql/` change. The configuration in `sqlc.yaml` emits Go bindings into `internal/infra/persistence/postgres/sqlc/`.

## Resetting the Development Database

Use the helper script to drop and recreate the `meltica` database locally:

```bash
./scripts/db/dev_reset.sh
```

Override the admin connection by exporting `DATABASE_URL` (defaults to `postgresql://postgres:root@localhost:5432/postgres?sslmode=disable`) or change the target database with `TARGET_DB`.
