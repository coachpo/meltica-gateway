# Meltica

**Package:** `github.com/coachpo/meltica`

Meltica is a Go 1.25 gateway for aggregating exchange market data, routing events through deterministic pipelines, and running lightweight trading lambdas. The codebase favors zero-copy transports, pooled objects, and explicit observability hooks.

## Supported Providers

| Provider          | Capabilities                                           | Notes                                                                                                     |
| ----------------- | ------------------------------------------------------ | --------------------------------------------------------------------------------------------------------- |
| Fake (synthetic)  | Spot-style ticks, trades, book snapshots, balances     | Ships with the repo for perf, regression, and contract testing.                                           |
| Binance (aliases) | Configuration scaffolding in `config/app.example.yaml` | The canonical adapter lives in a private module; aliases show how to register real venues when available. |

## Key Features

- Pooled `schema.Event`/`schema.OrderRequest` types managed by `internal/infra/pool` to cap allocation churn.
- In-memory fan-out bus (`internal/infra/bus/eventbus`) feeding dispatcher routes and strategy instances.
- Lambda runtime with a REST control plane (`docs/lambdas-api.md`) for creating, updating, and removing strategies on the fly.
- OTLP-ready telemetry provider (`internal/infra/telemetry`) plus curated Grafana dashboards under `docs/dashboards/` and metric definitions in `TELEMETRY_POINTS.md`.
- Configuration-driven provider registry, making it easy to alias multiple venues to a single adapter implementation.

## Architecture Overview

1. `cmd/gateway` is the entrypoint. It loads `config/app.yaml`, wires pools, the event bus, the dispatcher table, and HTTP control server, then blocks on OS signals.
2. `internal/app/provider` hosts the registry/manager that instantiates adapters registered via `internal/infra/adapters`. Built-in adapters register through the same hook.
3. `internal/app/dispatcher` maintains routing tables that map provider events into downstream fan-outs and strategy queues.
4. `internal/app/lambda/core` supplies the reusable lambda primitives, and `internal/app/lambda/runtime` spins strategies declared in the manifest or via the REST API, consuming events from the dispatcher and publishing responses back onto the bus.
5. `internal/infra/telemetry` configures OpenTelemetry exporters and propagates tracing/metrics context through the pipeline.

## Repository Layout

- `cmd/gateway`: Main binary; exposes `-config` to point at any `app.yaml`.
- `internal/`: Core implementation packages organised into `app/`, `domain/`, `infra/`, and `support/`.
- `api/`: Holds public API contracts and future protobuf/OpenAPI material.
- `config/`: Shipping configuration (`app.yaml`) plus `app.example.yaml` for local overrides.
- `deployments/`: IaC and telemetry deployment notes (`deployments/telemetry/PROMETHEUS_SETUP.md`).
- `docs/`: Lambdas API reference and Grafana dashboards.
- `test/` and `tests/`: Shared fixtures, architecture/contract suites, and package-level `_test.go` files.
- `scripts/`: Utility helpers for CI or local tooling.

## Quick Start

1. **Install dependencies**: Go 1.25+, `golangci-lint`, and (optionally) Docker/Prometheus if you plan to exercise telemetry pipelines.
2. **Configure the gateway**:
   ```bash
   cp config/app.example.yaml config/app.yaml
   # edit providers, telemetry endpoint, or lambda manifest as needed
   ```
3. **Run locally**:
   ```bash
   make run              # shorthand for go run ./cmd/gateway
   # or build & execute
   make build
   ./bin/gateway -config config/app.yaml
   ```
4. **Control the runtime**: Use the REST surface on `:8880` (see [`docs/lambdas-api.md`](docs/lambdas-api.md)) to list or mutate running strategies.
5. **Inspect telemetry**: Point OTLP endpoints at your collector and import the dashboards from `docs/dashboards/` into Grafana.

## Development Workflow

| Command                    | Purpose                                                            |
| -------------------------- | ------------------------------------------------------------------ |
| `make build`               | Compile all packages into `bin/` for local smoke tests.            |
| `make run`                 | Execute the gateway with the current configuration.                |
| `make test`                | Run `go test ./... -race -count=1 -timeout=30s` across the module. |
| `make lint`                | Execute `golangci-lint` with `.golangci.yml`.                      |
| `make coverage`            | Enforce the ≥70% TS-01 coverage bar and emit `coverage.out`.       |
| `make contract-ws-routing` | Run the focused contract suite in `tests/contract/ws-routing`.     |
| `make bench`               | Launch benchmark runs for hot paths.                               |

## Observability & Control References

- [`docs/lambdas-api.md`](docs/lambdas-api.md): REST contract for lifecycle operations.
- [`docs/dashboards/README.md`](docs/dashboards/README.md): How to import/update Grafana dashboards.
- [`deployments/telemetry/PROMETHEUS_SETUP.md`](deployments/telemetry/PROMETHEUS_SETUP.md): Collector wiring instructions.

## Contributor & Process Docs

- [`AGENTS.md`](AGENTS.md): Contributor guidelines and coding conventions.
- [`GEMINI.md`](GEMINI.md): Additional context for Gemini AI agents working inside the repo.
- [`CLAUDE.md`](CLAUDE.md): Additional context for Claude AI agents working inside the repo.

## License

MIT License - see [`LICENSE`](LICENSE) for details.
