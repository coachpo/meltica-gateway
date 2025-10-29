# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Meltica is a Go 1.25 gateway for aggregating exchange market data, routing events through deterministic pipelines, and running lightweight trading lambdas. The architecture favors zero-copy transports, pooled objects, and explicit observability via OpenTelemetry.

**Key Technologies:**
- Go 1.25+ with strict module management
- `github.com/goccy/go-json` for JSON serialization (standard `encoding/json` is banned)
- `github.com/coder/websocket` for WebSocket connections (gorilla/websocket is banned)
- OpenTelemetry for traces and metrics
- In-memory event bus with configurable fan-out workers

## Common Commands

### Build & Run
```bash
make build                  # Compile all packages into bin/
make run                    # Execute gateway with config/app.yaml
./bin/gateway -config <path>  # Run with custom config file
```

### Testing
```bash
make test                   # Run all tests with race detector (-race -count=1 -timeout=30s)
make coverage               # Enforce ≥70% coverage threshold (TS-01 requirement)
go tool cover -html=coverage.out  # View coverage report in browser
make contract-ws-routing    # Run WebSocket routing contract tests
go test ./internal/config -run TestLoad  # Run specific package tests
```

### Quality & Benchmarks
```bash
make lint                   # Run golangci-lint with .golangci.yml config
go vet ./...               # Run go vet static analysis
make bench                  # Run benchmark tests
```

### Single Test Execution
```bash
go test ./internal/dispatcher -run TestTable_Register -v
go test ./internal/pool -bench BenchmarkPoolManager -benchmem
```

## Architecture Overview

Meltica uses a four-layer pipeline architecture:

1. **Providers (`internal/provider`, `internal/adapters`)**: Manage exchange connections and emit normalized events. Each adapter (e.g., `fake`, `binance`) implements the `Provider` interface. The registry allows multiple provider aliases to map to a single adapter implementation.

2. **Event Bus (`internal/bus/eventbus`)**: In-memory fan-out system that distributes events from providers to dispatcher routes and strategy instances. Configured via `eventbus.bufferSize` and `eventbus.fanoutWorkers` in `config/app.yaml`.

3. **Dispatcher (`internal/dispatcher`)**: Maintains routing tables mapping `(provider, symbol, event_type)` tuples to downstream subscribers. The `Registrar` handles route registration from lambda instances.

4. **Lambda Runtime (`internal/lambda/runtime`)**: Manages strategy lifecycles declared in the manifest or created via REST API. Each lambda consumes events from the dispatcher and publishes order requests back to the bus.

### Key Data Flow
```
Provider → Events → Bus → Dispatcher Table → Routes → Lambda Strategies
                     ↓                                      ↓
                 Pool Manager ← OrderRequests ← Provider ← Bus
```

### Object Pooling System
`internal/pool` manages pooled `schema.Event` and `schema.OrderRequest` objects to minimize allocation churn in hot paths. Pools are configured in `config/app.yaml` under the `pools` section:
- `eventSize`: Event pool capacity (e.g., 50000)
- `orderRequestSize`: Order request pool capacity (e.g., 10000)

All event handlers must return pooled objects via `pool.Put()` to avoid leaks.

## Configuration System

**Single Source of Truth**: All configuration is loaded from a single YAML file via `config.Load(ctx, path)`.

### Configuration Structure
- `environment`: Deployment environment (dev, staging, prod)
- `providers`: Map of provider aliases to exchange-specific configuration blobs. Each entry must include an `exchange.name` field referencing a registered adapter. Aliases use the same `exchange.name` to share implementations.
- `eventbus`: Buffer size and fanout worker count
- `pools`: Event and OrderRequest pool capacities
- `apiServer`: Control API bind address (default `:8880`)
- `telemetry`: OTLP endpoint, service name, and enable flags
- `lambdaManifest`: Inline lambda definitions materialized at startup

**Example workflow:**
```bash
cp config/app.example.yaml config/app.yaml
# Edit providers, telemetry endpoint, or lambda manifest
make run
```

See `internal/config/README.md` for migration notes from the legacy fragmented configuration system.

## Lambda Strategies

Trading strategies live in `internal/lambda/strategies/` and implement the `TradingStrategy` interface with 8 event callbacks:
- `OnTrade`, `OnTicker`, `OnBookSnapshot`, `OnBalanceUpdate`, `OnOrderPlaced`, `OnOrderFilled`, `OnOrderCancelled`, `OnOrderRejected`

**Built-in Strategies:**
- `noop`: No-operation baseline
- `logging`: Logs all market events
- `delay`: Introduces configurable delays (useful for testing)
- `marketmaking`: Places orders around mid-price to capture spread
- `momentum`: Trades in direction of price movement
- `meanreversion`: Trades when price deviates from moving average
- `grid`: Places orders at regular price intervals

**Lambda Control API** (exposed on `:8880`):
- `GET /lambdas`: List running lambdas
- `POST /lambdas`: Create and start a new lambda
- `GET /lambdas/{id}`: Get lambda spec
- `PUT /lambdas/{id}`: Update lambda config (triggers restart)
- `DELETE /lambdas/{id}`: Stop and remove lambda

See `docs/lambdas-api.md` for full REST contract.

## Code Style & Standards

### Linting Rules
The project enforces strict linting via `.golangci.yml`:
- **Banned libraries**: `encoding/json` (use `goccy/go-json`), `gorilla/websocket` (use `coder/websocket`)
- **Enabled linters**: `wrapcheck`, `revive`, `nilerr`, `gosec`, `exhaustruct`, `exhaustive`, `forbidigo`, `depguard`
- **Forbidden patterns**: `legacy`, `deprecated`, `shim`, `feature_flag` identifiers

### Naming & Structure
- Use idiomatic Go: `gofmt`, `goimports`, tabs for indentation
- Public identifiers: PascalCase; internals: lowerCamelCase
- Keep side effects out of pure helpers; prefer dependency injection
- File names: lowercase with underscores only for testdata helpers

### Testing Guidelines
- Favor table-driven tests with descriptive suffixes: `TestRouteRegistry_Register/duplicateProvider`
- Keep fixtures deterministic in `testdata/` directories
- Architecture tests in `tests/architecture` guard layering rules
- Minimum coverage: 70% (enforced by `make coverage`)

### Commit Style
Use concise summaries in sentence case with imperative verbs:
- Good: "Propagate canonical route types"
- Bad: "propagated canonical route types", "WIP changes"

## Observability & Telemetry

- **Metrics & Traces**: Configured via `internal/telemetry` with OTLP exporters. See `TELEMETRY_POINTS.md` for enumeration of emitted metrics.
- **Grafana Dashboards**: Import from `docs/dashboards/` directory. See `docs/dashboards/README.md` for setup.
- **Prometheus Setup**: Collector wiring instructions in `deployments/telemetry/PROMETHEUS_SETUP.md`.
- **Semantic Conventions**: All telemetry follows conventions defined in `internal/telemetry/semconv.go`.

## Important Notes

- **No External Imports**: Code in `/internal` is compiler-enforced as private to this project.
- **In-Memory Only**: No persistence layer; all state is configuration-driven or runtime-managed in memory.
- **High-Performance Focus**: Object pooling, zero-copy transports, and efficient event routing are critical design principles.
- **Provider Aliases**: Multiple provider names in `config/app.yaml` can map to the same exchange adapter by using the same `exchange.name` value.

## Repository Layout

- `cmd/gateway`: Main binary entrypoint; loads `config/app.yaml`, wires pools, bus, dispatcher, and HTTP server
- `internal/`: Core implementation (adapters, config, dispatcher, event bus, pools, telemetry, schema)
- `config/`: Shipping configuration (`app.yaml`) and example (`app.example.yaml`)
- `api/`: Public API contracts and future protobuf/OpenAPI definitions
- `docs/`: Lambdas API reference, Grafana dashboards
- `deployments/`: Infrastructure-as-Code and telemetry deployment notes
- `test/`, `tests/`: Shared fixtures, architecture tests, contract suites
- `scripts/`: Utility helpers for CI or local tooling

## Additional Context

For deeper architectural context and contribution guidelines, refer to:
- `AGENTS.md`: Contributor guidelines and coding conventions
- `GEMINI.md`: AI agent context for navigating the codebase
- `MIGRATION.md`: Adapter and performance migration notes for v2 pooling/networking stack
- `internal/README.md`: Detailed breakdown of internal package structure
- `internal/lambda/strategies/README.md`: Strategy implementation guide with algorithm details
