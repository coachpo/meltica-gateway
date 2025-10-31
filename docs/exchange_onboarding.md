# Exchange Onboarding Guide

## Overview
This guide distills the Binance adapter workflow into practical steps for integrating additional exchanges. It covers provider responsibilities, event emission patterns, and how to hydrate order books from snapshot plus diff streams.

## WebSocket Stream Management (MANDATORY)

All exchange adapters **MUST** implement WebSocket stream management using the **Live Subscribing/Unsubscribing** pattern. This approach uses a single WebSocket connection per stream type and manages subscriptions dynamically via the exchange's native subscribe/unsubscribe API.

> **Rate-limit reminder:** Exchanges often impose per-connection limits on control traffic. Identify those caps during onboarding, then serialize SUBSCRIBE/UNSUBSCRIBE flows and pace control frames accordingly so reconnect storms stay under the venue’s thresholds and avoid `StatusPolicyViolation` disconnects.
> **Retry policy:** Always use exponential backoff for all retry scenarios.

## Provider Responsibilities
- Implement the provider interface with `Start(ctx)` guarding lifecycle and exposing `Events()`/`Errors()` channels backed by pooled `schema.Event` objects.
- Seed an instrument catalogue on startup and protect access with `sync.RWMutex`; normalize symbols using existing helpers.
- Maintain a **two-way symbol map** so canonical symbols (e.g. `BTC-USDT`) can be translated to the venue’s native identifiers (e.g. `BTCUSDT`) for requests and responses.
- Use `shared.Publisher` to emit canonical events, passing in the provider name, event channel, pool manager, and a monotonic clock.
- Ensure object pooling via `pool.PoolManager` to avoid allocations when publishing and returning schema instances, and fail fast (with clear logs) if a provider starts without an injected pool manager so misconfigurations surface immediately.
- Provide order-submission plumbing (even if initially a stub) so `SubmitOrder` can accept canonical `schema.OrderRequest` payloads and apply symbol/unit conversions.

## Adapter Metadata & Registration
- Define `publicMetadata` and `privateMetadata` helpers in `options.go`. The public struct should expose the adapter identifier, display name, venue code, and a short description; the private struct centralizes REST and WebSocket base URLs plus per-endpoint paths so future overrides stay isolated.
- Export a `provider.AdapterMetadata` instance (see `binanceAdapterMetadata`) that advertises capabilities and the `SettingsSchema`. Each entry in the schema must describe user-facing configuration keys (name, type, default, requirement, and purpose) because the control plane renders this metadata verbatim.
- When calling `RegisterWithMetadata` inside `manifest.go`, pass both the provider factory and the adapter metadata. The registry persists those descriptors for API discovery and config validation, so omitting them will block new adapters from being listed by the control API.
- Inside the factory, honor optional aliasing via `provider_name`/`name` keys and nest user configuration under `config` to remain consistent with existing manifests. Always run `withDefaults` so unspecified metadata-driven defaults propagate before instantiation.

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

## Instrument & Symbol Metadata
- Populate `symbolMeta`-style structs when building instruments. They should track the canonical symbol (`BTC-USDT`), the uppercase REST symbol (`BTCUSDT`), and the lowercase stream topic (`btcusdt`) so routing logic can translate requests, WebSocket subscriptions, and REST responses without recomputing strings.
- Store these metadata entries in a provider-level map keyed by canonical symbol and maintain the reverse lookup (`restToCanon`) for REST payloads. Refresh both maps whenever `refreshInstruments` runs to keep aliases and precision changes in sync with the venue.
- Use the metadata when seeding order books, submitting orders, and wiring stream subscriptions to guarantee that every code path resolves the same venue identifiers.

## Onboarding Checklist
- [ ] Implement adapter-specific options (REST/WebSocket clients, authentication, throttling, precision rules).
- [ ] Wire provider registration so aliases in `config/app.yaml` can reference the new exchange.
- [ ] Build a canonical↔venue symbol converter and reuse it across REST, WebSocket, and order submission paths.
- [ ] Publish tickers, trades, order book updates, balances, and execution reports using the shared publisher utilities.
- [ ] Confirm logging displays provider info so multi-provider deployments can trace control flow.
- [ ] Integrate the `OrderBookAssembler` when the exchange offers diff streams, falling back to periodic snapshots if necessary.
- [ ] Cover the adapter with table-driven unit tests and run contract suites such as `make contract-ws-routing` before merging.

## Detailed Implementation Notes

### Configuration & Options

- **Configuration Parsing:** Your provider's `manifest.go` should include a factory function that parses a `map[string]any` configuration into a strongly-typed `Options` struct. Use helper functions like `durationFromConfig`, `floatFromConfig`, and `intFromConfig` (see `binance/manifest.go`) to robustly handle different data types.
- **Default Values:** Establish sensible default values for all options in your `options.go` file. This ensures the provider can run with minimal configuration.

### State Management

- **Instrument State:** The `symbolMeta` and order-book management code in `binance/provider.go` demonstrate how to manage venue metadata, cached instruments, and diff replay. Your provider should maintain comparable structures tailored to the target venue.

### Order and Trade Logic

- **Order Management:** If the exchange supports trading, you'll need to implement order submission logic. This includes translating canonical `schema.OrderRequest` objects into exchange-specific payloads and handling different time-in-force (TIF) modes like GTC, IOC, and FOK. Order lifecycle tracking is typically handled by processing `ExecutionReport` events from the exchange's user data stream.
- **Instrument Constraints:** Implement robust handling of instrument constraints (see the Binance adapter's `buildInstrument` logic). This includes logic for:
    - Price and quantity increments.
    - Minimum and maximum order sizes.
    - Minimum notional value.
    - Price and quantity precision.
