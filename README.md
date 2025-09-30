# Meltica

**Package:** `github.com/coachpo/meltica`

A high-performance cryptocurrency exchange adapter framework written in Go.

> **Breaking Change (v2.0.0):** The legacy `market_data/framework/parser` package has been removed. Review the upgrade guide in [`BREAKING_CHANGES_v2.md`](./BREAKING_CHANGES_v2.md) before updating existing integrations.

## Supported Providers

| Exchange | Spot | Linear Futures | Inverse Futures | WebSocket Public | WebSocket Private |
|----------|------|----------------|-----------------|------------------|-------------------|
| Binance  | ✅   | ✅             | ✅              | ✅               | ✅                |

**Legend:** ✅ = Fully Supported

## Architecture

The system follows a formal four-layer architecture:

1. **Layer 1 – Connection** (`core/layers/connection.go`): WebSocket and REST transports; legacy provider shims have been removed in v2.
2. **Layer 2 – Routing** (`core/layers/routing.go`): Normalizes transport payloads, manages subscription lifecycles, and translates API requests.
3. **Layer 3 – Business** (`core/layers/business.go`): Coordinates domain workflows, maintains business state, and bridges routing outputs to filters.
4. **Layer 4 – Filter** (`core/layers/filter.go`): Final pipeline stage that transforms events for downstream clients and handles cleanup.

Supporting assets:
- `lib/ws-routing/`: Business-agnostic routing session, router, middleware, telemetry, and admin API packages imported via `github.com/coachpo/meltica/lib/ws-routing`.
- `tests/architecture/`: Contract tests, reusable mocks, and isolation examples.

See [`specs/008-architecture-requirements-req/quickstart.md`](specs/008-architecture-requirements-req/quickstart.md) for end-to-end guidance.

## Quick Start

```go
import (
    "context"
    "time"

    "github.com/coachpo/meltica/core/registry"
    binanceplugin "github.com/coachpo/meltica/exchanges/binance/plugin"
    wsrouting "github.com/coachpo/meltica/lib/ws-routing"
)

// Create a Binance provider
exchange, err := registry.Resolve(binanceplugin.Name)
if err != nil {
    log.Fatal(err)
}
defer exchange.Close()

// Use the exchange for market data or trading operations

ctx := context.Background()

// Configure a universal routing session via lib/ws-routing
session, err := wsrouting.Init(ctx, wsrouting.Options{
    SessionID: "binance-stream",
    Dialer:    myDialer,
    Parser:    myParser,
    Publish:   myPublisher,
    Backoff:   wsrouting.BackoffConfig{Initial: 250 * time.Millisecond},
})
if err != nil {
    log.Fatal(err)
}
```

## Features

- **Unified API**: Single interface for multiple exchanges (currently Binance)
- **High Performance**: Optimized for low-latency trading
- **Type Safety**: Strongly typed Go interfaces
- **Extensible**: Easy to add new exchanges following the Binance pattern
- **Production Ready**: Comprehensive error handling and logging
- **Typed Event Stream**: Clients consume strongly typed `ClientEvent` payloads instead of raw envelopes
- **Zero-Copy Transport**: Uses `github.com/coder/websocket` for context-aware WebSocket IO and `github.com/goccy/go-json` for fast serialization
- **Object Pooling**: Bounded pools backed by `sync.Pool` with 100ms acquisition timeouts and debug tooling reduce allocations by >40%

## Development

### Building

```bash
make build
```

### Testing

```bash
make test
```

### Cross-compilation

```bash
make build-linux-arm64
```

## Documentation

- [Getting Started](docs/getting-started/START-HERE.md)
- [Project Overview](docs/getting-started/PROJECT_OVERVIEW.md)
- [Adding New Exchanges](docs/guides/exchange-implementation/onboarding-new-exchange.md)
- [Architecture Standards](docs/standards/expectations/abstractions-guidelines.md)

## License

MIT License - see [LICENSE](LICENSE) for details.
