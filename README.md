# Meltica

Meltica is a Go 1.25 trading gateway that ingests market data, routes deterministic events, and executes lightweight JavaScript strategies. It emphasizes low-allocation pipelines, explicit observability hooks, and configuration-driven provider wiring so you can iterate on exchanges or fake adapters without code changes.

## Repository Layout

- `cmd/gateway`: main binary that loads configs from `config/app.yaml` (or `MELTICA_CONFIG_PATH`).
- `internal/app|domain|infra|support`: composition roots, domain entities, persistence & telemetry adapters, and shared helpers.
- `api/`, `docs/`, `deployments/telemetry/`: public contracts, lambda docs, and observability playbooks.
- `strategies/`: JavaScript reference strategies (git submodule) consumed by the gateway.
- `db/migrations` + `internal/infra/persistence/postgres/sqlc`: schema evolution plus generated repositories.

## Prerequisites

- Go 1.25 or newer on macOS or Linux
- PostgreSQL reachable through `DATABASE_URL`
- `golangci-lint` (needed for `make lint`)
- Optional: Docker/Prometheus if you plan to try telemetry flows

## Clone With Strategy Submodule

The gateway expects `strategies/` to be populated from `github.com/coachpo/meltica-strategy`. Clone Meltica with submodules so the JavaScript registry is available:

```bash
git clone --recurse-submodules git@github.com:coachpo/meltica.git
cd meltica
```

Already have the repo? Pull submodules with:

```bash
git submodule update --init --recursive
```

## Configure the Gateway

1. Copy and edit the sample config:
   ```bash
   cp config/app.example.yaml config/app.yaml
   # adjust providers, telemetry endpoints, pool sizing, strategy directory, etc.
   ```
   If you downloaded a release artifact (e.g., `meltica-v0.1.1-linux-amd64.zip`), the bundle ships with `config/app.example.yaml` in the `config/` directory—rename it to `config/app.yaml` before running the gateway unless you plan to point `MELTICA_CONFIG_PATH` elsewhere.
2. Provide secrets via env vars or `.env` (e.g., `DATABASE_URL`, OTLP exporter URLs, exchange keys).
3. Choose the config file location:
   - Default: `config/app.yaml`.
   - Override via `export MELTICA_CONFIG_PATH=/abs/path/config.yaml` or by passing `-config path/to/config.yaml` (e.g., `make run CONFIG_FILE=...`).

Key config sections (see `config/app.example.yaml`):

- `database`: DSN, pool sizing, optional auto-migrations.
- `eventbus` / `pools`: dispatcher buffer sizes and wait queues.
- `apiServer.addr`: control-plane HTTP endpoint.
- `telemetry`: OTLP endpoint, service name, metrics toggle.
- `strategies.directory`: directory Meltica watches for bundled strategies.

## Run Meltica

- Quick run (defaults to `config/app.yaml`):
  ```bash
  make run
  ```
  Provide `CONFIG_FILE=...` or set `MELTICA_CONFIG_PATH` when using a different config path.
- Build once, run manually:
  ```bash
  make build
  ./bin/gateway -config config/app.yaml
  ```
- Database migrations (reads `DATABASE_URL`):
  ```bash
  make migrate       # apply migrations in db/migrations
  make migrate-down  # roll back the last migration batch
  ```

## Helpful Targets & Tooling

| Command                    | Purpose                                                            |
| -------------------------- | ------------------------------------------------------------------ |
| `make run`                 | Build + start `cmd/gateway` using `CONFIG_FILE` or env override.   |
| `make build`               | Compile binaries into `./bin`.                                     |
| `make lint`                | Execute `golangci-lint` with `.golangci.yml`.                      |
| `make test`                | Run `go test ./... -race -count=1 -timeout=30s`.                   |
| `make coverage`            | Run tests with coverage and enforce the ≥70% bar.                  |
| `make bench`               | Execute benchmark suites for hot paths.                            |
| `make contract-ws-routing` | Contract test for websocket routing.                              |
| `make migrate`/`-down`     | Apply or roll back database migrations.                            |
| `sqlc generate`            | Rebuild typed PostgreSQL repositories.                             |

## Documentation

- `AGENTS.md`: repo conventions, coding style, and CI expectations.
- `docs/` + `deployments/telemetry/`: API notes, lambda docs, and OTLP wiring.
- `strategies/README.md`: strategy scaffolding and build instructions.

## License

Meltica is distributed under the MIT License (`LICENSE`).
