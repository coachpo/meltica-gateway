# JavaScript Strategy Runtime Guide

## Overview

The gateway now treats **all trading strategies as JavaScript (Goja) modules** that live under the configurable `strategies/` directory. The previous Go implementations (`internal/app/lambda/strategies/*.go`) have been retired; every strategy (noop, delay, logging, momentum, mean reversion, grid, market making) now runs through the Goja loader at runtime.

Key actors:

- `internal/app/lambda/js/loader.go` – scans the strategy directory, compiles modules, extracts metadata, and exposes CRUD helpers.
- `internal/app/lambda/js/strategy.go` – wraps a JS module in a `core.TradingStrategy`, wiring helper functions so JavaScript code can talk to the Go runtime.
- `internal/app/lambda/runtime/manager.go` – orchestrates lifecycle: loads modules at startup, launches lambdas, refreshes modules, and exposes HTTP endpoints.

All consumers (CLI, HTTP API, tests) use this pipeline to work with strategies.

---

## Strategy Life Cycle

1. **Startup**
   - `runtime.NewManager` builds a Goja loader pointing at `cfg.Strategies.Directory` (defaults to `strategies`).
   - It immediately calls `loader.Refresh(ctx)` and registers the metadata produced by each JS module.

2. **Create / Launch**
   - `Manager.launch` looks up the strategy identifier, instantiates the module with `js.NewStrategy`, creates a `BaseLambda`, calls `jsStrategy.Attach(base)` so helpers reach the lambda, then starts the lambda.

3. **Refresh**
   - `Manager.RefreshJavaScriptStrategies(ctx)` (also exposed via the `/strategies/refresh` endpoint) clears existing dynamic registrations, reloads modules from disk, and restarts any running instances that depend on the updated strategies.

4. **Stop / Shutdown**
   - `Manager.Stop(id)` cancels the strategy context, unregisters routes, and invokes the strategy’s `Close` method (implemented by `js.Strategy` to shut down the Goja goroutine).

---

## Goja Integration

### Loader (`internal/app/lambda/js/loader.go`)

- `NewLoader(dir)` – ensures the directory exists.
- `Refresh(ctx)` – recompiles each `*.js` file, extracts `module.exports.metadata`, and caches a `Module` containing the compiled program, metadata, and file info.
- `List`, `Module`, `Get`, `Read`, `Write`, `Delete` – helper methods to inspect/update modules. `Write` validates compilation before swapping the file into place.

### Strategy Wrapper (`internal/app/lambda/js/strategy.go`)

`NewStrategy(module, config, logger)` performs:

1. Creates a Goja runtime and populates an `envConfig`:
   - `helpers`: `log(...args)`, `sleep(duration)` (parses strings/numbers/durations and sleeps on the Go side).
   - `runtime` helpers from `lambdaBridge`:
     - `isTradingActive()`, `isDryRun()`
     - `providers()`, `selectProvider(seed)`
     - `submitMarketOrder(provider, side, size)`
     - `submitOrder(provider, side, size, price)`
     - `getLastPrice()`, `getBidPrice()`, `getAskPrice()`
     - `getMarketState()` → `{ last, bid, ask, spread, spreadPct }`
2. Calls the module’s `create(env)` export. The returned object must implement the strategy handlers (e.g., `onTrade`, `onOrderFilled`, etc.). Missing handlers default to no-ops.
3. Stores the Go object in `Strategy.handler`. All `core.TradingStrategy` methods call into JS via `lambdaBridge`.
4. `Strategy.Attach(base)` is invoked during launch to connect helper methods to the live `BaseLambda`.

Every JS exception is captured and logged via `Strategy.logError`.

---

## HTTP Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET`  | `/strategies` | List in-memory strategy metadata. |
| `GET`  | `/strategies/{name}` | Return metadata for a specific strategy. |
| `GET`  | `/strategies/modules` | Return module summaries (filename, hash, metadata). |
| `POST` | `/strategies/modules` | Create a module. Body: `{"filename":"...", "source":"..."}`. Validates compilation before writing. |
| `GET`  | `/strategies/modules/{name}` | Fetch metadata/file info for a module by strategy name. |
| `GET`  | `/strategies/modules/{name}/source` | Return raw JS source (`application/javascript`). Accepts canonical name or filename. |
| `PUT`  | `/strategies/modules/{name}` | Replace strategy source. Same payload as POST. |
| `DELETE` | `/strategies/modules/{name}` | Remove module file. |
| `POST` | `/strategies/refresh` | Reload modules from disk and restart affected lambdas. |

All errors surface as standard HTTP codes (400, 404, 409, etc.). Module CRUD endpoints touch disk; reload is required before changes take effect in memory.

---

## Creating a JavaScript Strategy

1. **Create the file** in the strategies directory. Template:

   ```js
   module.exports = {
     metadata: {
       name: "my-strategy",
       displayName: "My Strategy",
       description: "Short description",
       config: [
         { name: "threshold", type: "float", default: 0.5, required: false }
       ],
       events: ["Trade", "ExecReport"]
     },
     create: function (env) {
       const log = (...args) => env.helpers.log("[MYSTRAT]", ...args);
       const { runtime, config } = env;
       return {
         wantsCrossProviderEvents: () => false,
         onTrade: function (ctx, evt, tradePayload, price) {
           if (!runtime.isTradingActive()) return;
           const provider = runtime.selectProvider(Date.now());
           // strategy logic...
         },
         onOrderFilled: function (ctx, evt, report) {
           log("filled", report.side, report.avgFillPrice);
         }
       };
     }
   };
   ```

   - `metadata.name` is the identifier used in manifests and API calls.
   - `events` must match canonical schema event names (`"Trade"`, `"ExecReport"`, etc.).
   - Use the injected helpers to log, sleep, read market state, and submit orders.

2. **Upload the module** via `POST /strategies/modules` or drop it into the directory and call `POST /strategies/refresh`.

3. **Reference it** in a lambda manifest or runtime API calls using `metadata.name`.

4. **Validate** with `go test ./...` or by running the backtest CLI (see below).

---

## Backtesting (cmd/backtest)

The CLI now:

1. Loads the strategy directory (`-strategies.dir`, default `strategies`).
2. Picks the module specified with `-strategy` (e.g., `noop`, `grid`).
3. Instantiates the JS module with the provided configuration (`grid.levels`, `grid.spacing`, `grid.orderSize`, etc.).
4. Attaches it to a `BaseLambda`, enables trading, and runs the CSV data through `backtest.NewEngine`.

Running the CLI no longer relies on Go strategy structs; all behavior flows through the Goja runtime.

---

## Tests & Validation

- `go test ./...` rebuilds the loader, runtime, HTTP endpoints, and backtest harness using only the JS strategy pipeline.
- Runtime and HTTP server tests now inject `config.Strategies.Directory = "../.../strategies"` so the loader sees the repository’s JS fixtures.
- Backtest tests use a helper (`loadTestStrategy`) that resolves the `noop` JS module and ensures it is wired into a `BaseLambda`.

This guarantees repository CI exercises the Goja strategy path end-to-end.

---

## Operational Notes

- **Configuration**: `config/app.yaml` sets `strategies.directory`. Point this to the directory that houses production strategy files.
- **Deployment**: The loader reads files at startup. File changes require an explicit refresh (HTTP `POST /strategies/refresh` or calling `Manager.RefreshJavaScriptStrategies`).
- **Fallbacks**: No Go strategies remain registered. If the strategies directory is empty or the loader fails, the strategy catalog is empty and strategy creation will fail with “strategy not registered”.
- **Error handling**: Exceptions thrown within JS handlers bubble out of Goja; the bridge logs them through `Strategy.logError` along with the strategy name and method.

With these changes, strategies can be edited, hot-loaded, and iterated without recompiling the Go binaries. Use the helper surface to integrate JS logic cleanly with Go infrastructure, and leverage the REST endpoints for lifecycle management.

---

## Capabilities & Limitations of JavaScript Strategies

### What You Can Do

- **Access runtime context** through the injected helpers:
  - Query trading status, providers, market state, and prices.
  - Submit market or limit orders via the bridge.
  - Use logging and sleep utilities in Goja.
- **Maintain arbitrary in-memory state** inside the module (arrays, objects, timers).
- **Implement any business logic** expressible in ECMAScript 5.1+ (Goja’s supported subset), including deterministic math, state machines, and orchestration based on events.
- **Share code via CommonJS patterns** (e.g., require) as long as you bundle the modules in the strategy directory and load them before refresh.

### What You Cannot Do (Current Limitations)

- **No direct access to Go libraries** beyond the exposed helper surface. You cannot import Go packages or call arbitrary Go functions from JS.
- **Limited standard library**: Goja implements ES5.1 (+ some ES6). Features like `fetch`, `setTimeout`, or Node.js APIs are not available unless explicitly polyfilled.
- **No built-in I/O or network access**: the runtime executes inside the Go process without direct filesystem, network, or database connectors. If you need database access, you must expose that functionality via additional Go helpers; the JS module itself cannot open network connections or sockets.
- **Synchronous execution only**: the helper bridge runs callbacks on a single goroutine queue; long-running/blocking operations inside JS will stall other handlers. Use the provided `helpers.sleep` for delays instead of busy waits.
- **Resource isolation**: each strategy runs in its own Goja VM, but shares the process heap. Avoid unbounded memory growth; the runtime does not enforce per-strategy memory limits.
- **No automatic versioning or sandboxing** out of the box—strategy promotions and tagging must be handled externally (see the strategy versioning plan for a proposed upgrade).

If you need capabilities outside this surface (e.g., database CRUD, REST calls), expose them as Go helper functions in the bridge—take care to keep them deterministic and to manage blocking semantics so that strategy handlers remain responsive.
