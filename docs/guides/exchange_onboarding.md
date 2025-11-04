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
Most venues deliver books using some combination of snapshots and incremental diffs. Handle each pattern explicitly:

- **REST snapshot + streaming diffs** – Fetch a REST snapshot that includes a sequence identifier, feed it into `OrderBookAssembler.ApplySnapshot`, then stream WebSocket diffs through `ApplyDiff`. The assembler will buffer diffs that arrive before the snapshot, discard stale updates, and return fresh `schema.BookSnapshotPayload` instances whenever a diff is applied. Binance’s spot books follow this pattern (`/api/v3/depth` + `@depth` stream).
- _Example_: call Binance `/api/v3/depth?symbol=BTCUSDT` to obtain a snapshot with `lastUpdateId=1024`, seed the assembler, then push each incoming WebSocket diff (`U=1025`, `u=1026`, `bids=[...]`) into `ApplyDiff` and publish the returned snapshot.
- **WebSocket snapshot bootstrapping** – Some channels send a full snapshot as the very first WebSocket message. Treat that payload as the initial snapshot: publish it immediately, seed the assembler with `ApplySnapshot`, and then apply subsequent diff messages exactly as above. No REST round-trip is required unless resyncing. OKX’s `books` channel behaves this way.
- _Example_: after subscribing to OKX `books`, the first message arrives as `{action:"snapshot", data:[{bids:[...], asks:[...], seqId:9000}]}`; emit it, seed the assembler with `ApplySnapshot(9000, payload)`, then process the next `{action:"update", prevSeqId:9000, seqId:9001}` diff.
- **Resync after gaps** – If you detect missed sequences (e.g. `prevSeq` does not match the last applied sequence, zero sequence IDs, or checksum mismatches), set the handle into a resync state, fetch a fresh snapshot, and replay diffs once the assembler reports it is initialized again.
- _Example_: when a diff arrives with `prevSeqId=9100` but the assembler last stored `seq=9098` (common on reconnects for either Binance or OKX), trigger `fetchOrderBookSnapshot`, reapply `ApplySnapshot`, and resume applying buffered diffs once `lastSeq` catches up.

> **Heads-up:** Several exchanges omit the instrument identifier inside snapshot/diff records and rely on the subscription arguments or envelope metadata instead. Preserve that context so you can resolve the canonical symbol before publishing events.

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
- [ ] **Implement trading support** if the exchange offers trading APIs (see Trading Support section below).
- [ ] Cover the adapter with table-driven unit tests and run contract suites such as `make contract-ws-routing` before merging.
- [ ] Run `make lint` and resolve all linting issues (especially exhaustruct warnings for struct initialization).

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

## Trading Support (CRITICAL for Strategy Compatibility)

If the exchange adapter is intended to support trading strategies (e.g., grid, market making, momentum), you **MUST** implement the following components. Missing any of these will prevent strategies from functioning correctly.

### 1. API Credentials Configuration

Add trading credentials to `options.go`:
```go
type Config struct {
    Name              string
    APIKey            string        // Required for authenticated requests
    APISecret         string        // Required for request signing
    Passphrase        string        // Required for some exchanges (e.g., OKX)
    // ... other fields
}
```

Update `AdapterMetadata.SettingsSchema` to include:
- `api_key` (string) - API key for authenticated REST and user data streams
- `api_secret` (string) - API secret for signing REST requests
- `passphrase` (string) - API passphrase if required by the exchange

Update `AdapterMetadata.Capabilities` to include `"trading"`.

### 2. REST Order Submission

Implement `submitOrder()` method in `rest.go`:
- Parse `schema.OrderRequest` and convert to exchange-specific format
- Implement exchange-specific authentication (HMAC signing, headers, etc.)
- Handle order types (market, limit) and side (buy, sell)
- Support optional fields like client order ID and price
- Return proper error messages for validation failures

Example authentication patterns:
- **Binance:** HMAC-SHA256 signature in query parameters with `X-MBX-APIKEY` header
- **OKX:** HMAC-SHA256 signature in headers (`OK-ACCESS-SIGN`, `OK-ACCESS-KEY`, `OK-ACCESS-TIMESTAMP`, `OK-ACCESS-PASSPHRASE`)

Update `SubmitOrder()` in `provider.go` to call `submitOrder()` instead of returning "not supported" error.

### 3. Private WebSocket Stream (User Data)

Implement user data stream to receive real-time updates:
- Create separate WebSocket manager for private streams (authenticated)
- Implement exchange-specific login/authentication flow
- Subscribe to order updates and account balance channels
- Handle reconnection and re-authentication on disconnect

Add to `provider.go`:
```go
type Provider struct {
    // ... existing fields
    privateWsMu     sync.Mutex
    privateWs       *wsManager
    userStreamReady bool
    balanceMu       sync.Mutex
    balances        map[string]BalanceItem
}
```

Start user data stream in `Start()` if credentials are configured:
```go
if p.hasTradingCredentials() {
    go p.startUserDataStream()
}
```

### 4. Execution Report Handling

Implement `handleOrders()` to process order update events:
- Parse exchange-specific order update messages
- Map exchange order states to `schema.ExecReportState` (ACK, PARTIAL, FILLED, CANCELLED, REJECTED, EXPIRED)
- Populate `schema.ExecReportPayload` with all required fields:
  - `ExchangeOrderID`, `ClientOrderID`
  - `State`, `Side`, `OrderType`
  - `Price`, `Quantity`, `FilledQuantity`, `RemainingQty`
  - `AvgFillPrice`, `Timestamp`
  - Optional: `CommissionAmount`, `CommissionAsset`, `RejectReason`
- Publish via `p.publisher.PublishExecReport()`

**Critical:** Strategies rely on execution reports to track order lifecycle. Missing or incorrect state mappings will cause strategies to malfunction.

### 5. Balance Update Handling

Implement `handleAccount()` to process balance updates:
- Parse exchange account/balance update messages
- Update local balance cache
- Publish `schema.BalanceUpdatePayload` with:
  - `Currency`, `Available`, `Total`, `Timestamp`
- Implement `publishBalanceSnapshot()` for initial balance sync on startup

### 6. Helper Functions

Add utility functions to `provider.go`:
```go
func (p *Provider) hasTradingCredentials() bool {
    return strings.TrimSpace(p.opts.Config.APIKey) != "" &&
           strings.TrimSpace(p.opts.Config.APISecret) != ""
}
```

Add to `manifest.go` config parsing:
```go
if raw, ok := stringFromConfig(userCfg, "api_key"); ok {
    opts.Config.APIKey = raw
}
if raw, ok := stringFromConfig(userCfg, "api_secret"); ok {
    opts.Config.APISecret = raw
}
```

### 7. Common Pitfalls (Lessons from OKX Onboarding)

1. **Stub SubmitOrder:** Initial implementation had `return errors.New("trading not supported")` - this must be replaced with actual implementation.

2. **Missing Private WebSocket:** Market data streams are separate from user data streams. Trading requires a second authenticated WebSocket connection.

3. **Authentication Flow:** Each exchange has unique authentication:
   - Some use query string signatures (Binance)
   - Others use header-based signatures (OKX)
   - Some require login messages on WebSocket (OKX)
   - Some use listen keys (Binance)

4. **State Mapping:** Exchange order states don't directly map to schema states:
   - OKX "live" → `ExecReportStateACK`
   - OKX "partially_filled" → `ExecReportStatePARTIAL`
   - OKX "filled" → `ExecReportStateFILLED`
   - OKX "canceled" → `ExecReportStateCANCELLED`

5. **Struct Exhaustiveness:** Use linter-compliant struct initialization with all fields explicitly set (including empty strings for optional fields) to avoid exhaustruct warnings.

6. **Balance vs Account Updates:** Balance updates can come from both balance-specific channels and order update channels. Deduplicate carefully.

7. **Orderbook Events:** Market data events (orderbook, trades, tickers) are independent from trading support but must work correctly for strategy initialization.

8. **WebSocket Ping/Pong Protocol:** Each exchange has different WebSocket keepalive requirements:
   - **Binance:** Supports standard WebSocket ping control frames via `conn.Ping()`
   - **OKX:** Requires sending plain text `"ping"` (not JSON) and expects `"pong"` response
   - Always check exchange documentation for specific ping/pong format

9. **Orderbook Sequence IDs:** Some exchanges don't provide sequence IDs for full orderbook snapshots:
   - **Binance:** Always provides `lastUpdateId` in REST responses
   - **OKX:** May return empty `seqId` for full orderbook requests
   - Use timestamp as fallback sequence ID when `seqId` is missing
   - Handle missing/empty sequence fields gracefully to avoid parsing errors

10. **JSON Type Inconsistencies:** Exchanges may send the same field with different JSON types:
    - **OKX:** Sends `seqId` as **number** in WebSocket messages but as **string** in REST responses
    - **Solution:** Use `json.Number` type for fields that may be sent as either string or number
    - Example: `SeqID json.Number \`json:"seqId"\`` can unmarshal both `"123"` and `123`
    - Convert to string with `.String()` method: `seqStr := event.SeqID.String()`

### Verification Checklist

After implementing trading support, verify:
- [ ] `make lint` passes without errors
- [ ] `make build` compiles successfully
- [ ] Strategies can retrieve initial market data (trades, tickers, orderbook)
- [ ] Strategies can submit orders via `SubmitOrder()`
- [ ] Execution reports are received and processed
- [ ] Balance updates are received and cached
- [ ] Order state transitions are correctly mapped
- [ ] Reconnection preserves trading functionality
