# Meltica

Meltica is a Go 1.25 trading gateway that aggregates market data, dispatches deterministic events, and runs lightweight JavaScript lambdas. The project prioritizes low-allocation pipelines, explicit observability hooks, and configuration-driven provider setup.

## Prerequisites

- Go 1.25 or newer on macOS or Linux
- PostgreSQL (local or remote) accessible via `DATABASE_URL`
- `golangci-lint` for linting (optional for runtime, required for CI parity)
- Optional: Docker/Prometheus if you plan to exercise telemetry flows

## Clone With Strategies Submodule

Meltica ships JavaScript example strategies as a git submodule (`github.com/coachpo/meltica-strategy`). Clone or update the repo with submodules before building so the gateway can load registry data from `strategies/`.

```bash
git clone --recurse-submodules git@github.com:coachpo/meltica.git
cd meltica
```

Already cloned? Pull the strategies repo with:

```bash
git submodule update --init --recursive
```

## Configure Meltica

1. Copy the example config and adjust it for your environment:
   ```bash
   cp config/app.example.yaml config/app.yaml
   # edit providers, telemetry endpoints, and lambda manifests as needed
   ```
2. (Optional) Provide database/telemetry secrets via `.env` or environment variables such as `DATABASE_URL`.
3. Point Meltica at a config file:
   - Default: `config/app.yaml`
   - Override via `MELTICA_CONFIG_PATH` or pass `-config` to the binary.

## Running the Gateway

### Quick run
```bash
make run                               # builds and runs cmd/gateway with CONFIG_FILE or MELTICA_CONFIG_PATH
```

### Build + execute
```bash
make build                             # compiles binaries into ./bin/
./bin/gateway -config config/app.yaml  # starts the gateway using your config
```

### Database migrations
```bash
make migrate       # apply migrations using DATABASE_URL
make migrate-down  # roll back the last migration
```

## Useful Make Targets

| Command                    | Purpose                                                            |
| -------------------------- | ------------------------------------------------------------------ |
| `make build`               | Compile all Meltica packages into `bin/`.                          |
| `make run`                 | Run `cmd/gateway` using `CONFIG_FILE` or `MELTICA_CONFIG_PATH`.     |
| `make test`                | Execute `go test ./... -race -count=1 -timeout=30s`.               |
| `make lint`                | Run `golangci-lint` via `.golangci.yml`.                            |
| `make coverage`            | Run tests with coverage and enforce the ≥70% TS-01 bar.            |
| `make migrate`/`-down`     | Apply or roll back database migrations.                            |
| `make contract-ws-routing` | Execute the websocket routing contract test suite.                 |
| `make bench`               | Run benchmark suites for hot paths.                                |
| `sqlc generate`            | Regenerate typed PostgreSQL repositories.                          |

## Documentation & Telemetry

- [`docs/lambdas-api.md`](docs/lambdas-api.md): REST control-plane reference for managing strategies.
- [`docs/dashboards/`](docs/dashboards/README.md): Importable Grafana dashboards for Meltica metrics.
- [`deployments/telemetry/PROMETHEUS_SETUP.md`](deployments/telemetry/PROMETHEUS_SETUP.md): Guidance for wiring OTLP collectors.
- [`AGENTS.md`](AGENTS.md): Repository conventions and coding standards.

## License

Meltica is distributed under the MIT License. See [`LICENSE`](LICENSE) for details.
