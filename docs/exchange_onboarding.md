# Exchange Onboarding Guide

## Overview
This guide distills the fake provider workflow into practical steps for integrating real exchanges. It covers provider responsibilities, event emission patterns, and how to hydrate order books from snapshot plus diff streams.

## WebSocket Stream Management (MANDATORY)

All exchange adapters **MUST** implement WebSocket stream management using the **Live Subscribing/Unsubscribing** pattern. This approach uses a single WebSocket connection per stream type and manages subscriptions dynamically via the exchange's native subscribe/unsubscribe API.

> **Rate-limit reminder:** Exchanges often impose per-connection limits on control traffic. Identify those caps during onboarding, then serialize SUBSCRIBE/UNSUBSCRIBE flows and pace control frames accordingly so reconnect storms stay under the venue’s thresholds and avoid `StatusPolicyViolation` disconnects.
> **Retry policy:** Always use exponential backoff for all retry scenarios.

## Provider Responsibilities
- Implement the provider interface with `Start(ctx)` guarding lifecycle and exposing `Events()`/`Errors()` channels backed by pooled `schema.Event` objects.
- Seed an instrument catalogue on startup and protect access with `sync.RWMutex`; normalize symbols using existing helpers.
- Maintain a **two-way symbol map** so canonical symbols (e.g. `BTC-USDT`) can be translated to the venue’s native identifiers (e.g. `BTCUSDT`) for requests and responses.
- Use `shared.Publisher` to emit canonical events, passing in the provider name, event channel, pool manager, and a monotonic clock.
- Ensure object pooling via `pool.PoolManager` to avoid allocations when publishing and returning schema instances.
- Provide order-submission plumbing (even if initially a stub) so `SubmitOrder` can accept canonical `schema.OrderRequest` payloads and apply symbol/unit conversions.

## Event Emission Patterns
### Tickers
- Resolve an instrument state (creating it on first access) and advance price via `nextPrice` for deterministic drift.
- Derive bid/ask by applying a small spread around the last price, format values with exchange precision, and publish with `PublishTicker`.

### Trades
- Reuse instrument state lookup, call `nextPrice`, normalize quantities against instrument constraints, update rolling 24h volume, and emit via `PublishTrade` with unique trade IDs.

### Order Execution Reports
- Populate missing fields (exchange order ID, price, quantity, timestamp) with sensible defaults and publish with `PublishExecReport` to mirror venue acknowledgements.
- Ensure execution and balance updates reflect venue-side identifiers, then convert back to canonical form before publishing.

### Order Book State
- Maintain per-symbol `symbolMarketState` with synchronized bid/ask maps keyed by normalized price ticks.
- Support order resting, liquidity consumption, and price derivation using constraint helpers for consistent precision handling.

## Order Book Assembly From Snapshot + Diffs
1. Request a REST snapshot that includes a sequence identifier. Invoke `OrderBookAssembler.ApplySnapshot`, which resets internal maps, applies the snapshot, and replays any buffered diffs.
2. Stream diff messages into `ApplyDiff`. The assembler buffers them until the initial snapshot lands, ignores stale sequences, and applies updates atomically.
3. Consume the returned `schema.BookSnapshotPayload` from each successful `ApplyDiff` as the canonical book. If you detect dropped or out-of-order sequences, fetch a new snapshot and call `ApplySnapshot` again to resynchronize.

## Onboarding Checklist
- [ ] Implement adapter-specific options (REST/WebSocket clients, authentication, throttling, precision rules).
- [ ] Wire provider registration so aliases in `config/app.yaml` can reference the new exchange.
- [ ] Build a canonical↔venue symbol converter and reuse it across REST, WebSocket, and order submission paths.
- [ ] Publish tickers, trades, order book updates, balances, and execution reports using the shared publisher utilities.
- [ ] Integrate the `OrderBookAssembler` when the exchange offers diff streams, falling back to periodic snapshots if necessary.
- [ ] Cover the adapter with table-driven unit tests and run contract suites such as `make contract-ws-routing` before merging.

## Detailed Implementation Notes

### Configuration & Options

- **Configuration Parsing:** Your provider's `manifest.go` should include a factory function that parses a `map[string]any` configuration into a strongly-typed `Options` struct. Use helper functions like `durationFromConfig`, `floatFromConfig`, and `intFromConfig` (see `fake/manifest.go`) to robustly handle different data types.
- **Default Values:** Establish sensible default values for all options in your `options.go` file. This ensures the provider can run with minimal configuration.

### State Management

- **Instrument State:** The `symbolMarketState` struct (see `fake/state.go`) is central to managing all data related to a specific instrument, including its order book, last price, and Kline data. Your provider should maintain a map of these states.
- **Kline/Candlestick Data:** Implement logic to handle Kline data. This typically involves:
    - A `klineWindow` struct to represent a single candlestick.
    - An `updateKline` function to update the current Kline window with new trades.
    - A `finalizeKlines` function to close completed Kline windows and move them to a completed list.

### Order and Trade Logic

- **Order Management:** If the exchange supports trading, you'll need to manage the lifecycle of orders. This includes:
    - An `activeOrder` struct to represent an order on the exchange.
    - Logic to handle different time-in-force (TIF) modes like GTC, IOC, and FOK.
- **Instrument Constraints:** Implement robust handling of instrument constraints (see `fake/constraints.go`). This includes logic for:
    - Price and quantity increments.
    - Minimum and maximum order sizes.
    - Minimum notional value.
    - Price and quantity precision.
