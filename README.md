# Meltica Gateway

Meltica is a Go 1.25 trading gateway that ingests market data, routes deterministic events, and executes lightweight JavaScript strategies. It focuses on low-allocation pipelines, explicit observability hooks, and configuration-driven provider wiring so you can swap exchanges or fake adapters without code changes.

## Overview
- **Entrypoint**: `cmd/gateway` loads configuration from `config/app.yaml` or `MELTICA_CONFIG_PATH`, applies migrations, starts telemetry, spins up providers, and exposes the control API.
- **Architecture**: Internal packages separate orchestration (`internal/app`), domain entities (`internal/domain`), infrastructure adapters (`internal/infra`), and helpers (`internal/support`). JS strategy runtime lives under `internal/app/lambda`.
- **Persistence & schema**: Postgres migrations live in `db/migrations`; sqlc-generated repositories live in `internal/infra/persistence/postgres/sqlc`.
- **Contracts & docs**: Public APIs in `api/`; operational docs and dashboards in `docs/` and `deployments/telemetry/`.
- **Strategies**: Drop reference strategies in `strategies/` or point `strategies.directory` elsewhere. Experimental adapters live under `hypnotism/` and `strategies/` submodules when present.

## Project Layout
- `cmd/gateway` — gateway binary and CLI flags (`-config`, env `MELTICA_CONFIG_PATH`).
- `cmd/migrate` — migration runner used by `make migrate`.
- `internal/app` — dispatcher, lambda runtime, providers, pools.
- `internal/domain` — canonical schemas and error envelopes.
- `internal/infra` — adapters, event bus, config loader, HTTP server, telemetry, postgres repos.
- `internal/support` — reserved utility space; `internal/testutil` for fixtures.
- `api/` — control-plane contracts.
- `db/migrations` — SQL migrations + `embed.go`.
- `deployments/telemetry` — OTLP/Prometheus/Grafana setup.
- `docs/` — API changes, roadmap, guides, dashboards.
- `tests/contract` — cross-package contract suites; unit tests live beside code.
- `config/` — `app.yaml`, `app.example.yaml`, `app.ci.yaml`.

## Prerequisites
- Go 1.25+
- PostgreSQL reachable via `DATABASE_URL`
- `golangci-lint` for `make lint`
- (Optional) Docker/Prometheus/Grafana for telemetry experiments

## Quickstart
```bash
git clone git@github.com:coachpo/meltica-gateway.git
cd meltica-gateway
cp config/app.example.yaml config/app.yaml   # adjust DSN, telemetry, pools, strategy directory
export DATABASE_URL=postgresql://postgres:root@localhost:5432/meltica?sslmode=disable
make run                                     # or: make run CONFIG_FILE=path/to/app.yaml
```
Use `MELTICA_CONFIG_PATH=/abs/path/app.yaml` or `-config` to point at a non-default config.

## Configuration
Key sections in `config/app.yaml` / `.example.yaml`:
- `environment` — `dev|staging|prod|ci`
- `database` — `dsn`, pool sizing, `runMigrations` toggle
- `eventbus` and `pools` — buffer sizes and wait queues for dispatcher and order requests
- `apiServer.addr` — control API bind address (e.g., `:8880`)
- `telemetry` — `otlpEndpoint`, `serviceName`, `otlpInsecure`, `enableMetrics`
- `strategies.directory` — where strategy JS bundles are read from; `requireRegistry` in CI config

## Development Commands
```bash
make run                         # go run ./cmd/gateway -config config/app.yaml
make build                       # build binaries into ./bin
make build-linux-arm64           # cross-compile + stage configs in bin/linux-arm64
make lint                        # golangci-lint using .golangci.yml
make test                        # go test ./... -race -count=1 -timeout=30s
make coverage                    # enforces >=70% coverage, writes coverage.out
make bench                       # benchmark suites
make migrate                     # apply db/migrations using DATABASE_URL
make migrate-down                # roll back last migration batch
make sqlc                        # regenerate postgres repositories (sqlc generate)
```
Environment knobs:
- `CONFIG_FILE` (make) or `-config` flag / `MELTICA_CONFIG_PATH` env to choose config file.
- `DATABASE_URL` for migrations and runtime DB access.
- `MIGRATE_BIN` to override the migration runner (defaults to `go run ./cmd/migrate`).

## Database & Migrations
- Migrations live in `db/migrations` (up/down SQL plus `embed.go` for bundling).
- `make migrate` / `make migrate-down` run against `DATABASE_URL`. CI uses `config/app.ci.yaml` with auto-migrations enabled.

## Telemetry
- Configure OTLP endpoint and service name in `telemetry` config block.
- Ready-to-run Prometheus + Grafana + OTEL Collector manifests live in `deployments/telemetry/` (`docker-compose.yml`, `PROMETHEUS_SETUP.md`, etc.).

## Testing
- Unit/integration tests: `make test`.
- Coverage gate: `make coverage` enforces ≥70% (TS-01).
- Contract suites live under `tests/contract`; add fixtures in `test/` if needed.

## Strategy Development
- Place strategy bundles in `strategies/` or point `strategies.directory` to another path.
- Runtime registers strategies via the dispatcher and lambda manager; see `internal/app/lambda` for lifecycle details.

## Code Generation
- `sqlc generate` (via `make sqlc`) refreshes typed Postgres repositories in `internal/infra/persistence/postgres/sqlc`.

## Deployment Notes
- See `deployments/` for Docker/Kubernetes/Terraform/Ansible scaffolding.
- CI config (`config/app.ci.yaml`) disables telemetry and tightens pool sizes; set `MELTICA_CONFIG_PATH=config/app.ci.yaml` in CI runs.

## License
MIT License (`LICENSE`).
