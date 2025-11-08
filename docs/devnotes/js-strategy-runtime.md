# JavaScript Strategy Runtime Guide

## Overview

The gateway now treats **all trading strategies as JavaScript (Goja) modules** that live under the configurable `strategies/` directory. Each strategy is stored as a **versioned module** inside the directory structure and declared in a shared `registry.json`. The previous Go implementations (`internal/app/lambda/strategies/*.go`) have been retired; every strategy (noop, delay, logging, momentum, mean reversion, grid, market making) now runs through the Goja loader at runtime.

Key actors:

- `internal/app/lambda/js/loader.go` – scans the strategy directory, compiles modules, extracts metadata, and exposes CRUD helpers.
- `internal/app/lambda/js/strategy.go` – wraps a JS module in a `core.TradingStrategy`, wiring helper functions so JavaScript code can talk to the Go runtime.
- `internal/app/lambda/runtime/manager.go` – orchestrates lifecycle: loads modules at startup, resolves human-friendly selectors to immutable hashes, launches lambdas, refreshes modules, and exposes HTTP endpoints.

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
   - The HTTP endpoint now accepts an optional JSON payload `{ "hashes": [], "strategies": [] }` to target specific revisions. The response enumerates each requested selector with a reason code (`refreshed`, `alreadyPinned`, `retired`) so operators can confirm which instances actually restarted.

4. **Stop / Shutdown**
   - `Manager.Stop(id)` cancels the strategy context, unregisters routes, and invokes the strategy’s `Close` method (implemented by `js.Strategy` to shut down the Goja goroutine).

---

## Versioned Modules & Selectors

- Every module lives under `strategies/<name>/<tag>/<name>.js` and is tracked in `strategies/registry.json`.
- The loader reads the registry on refresh and computes the SHA-256 hash for every revision. The registry maps:
  - **Tags** (e.g., `v1.0.0`, `latest`) → hashes
  - **Hashes** (e.g., `sha256:…`) → on-disk path + primary tag
- Runtime selectors resolve as follows:
  - `name` → the module referenced by `tags.latest`
  - `name:tag` → the hash mapped to that tag
  - `name@hash` → the exact revision (hash must exist)
- When a strategy instance launches, the manager pins the resolved hash in the stored spec. Subsequent refreshes only restart the instance if the underlying hash changes.
- Module listings (`/strategies/modules`) now include `tagAliases` and per-revision metadata so operators can audit which hashes exist and which tags point to them.
- Deleting a revision (`DELETE /strategies/modules/{selector}`) is guarded: the manager refuses to remove a hash while any instance still references it.

For teams upgrading from the legacy flat layout, use `scripts/bootstrap_strategies` to reorganize existing files and emit a registry before enabling the new flow (see Operational Notes).

### Managing Tags

1. Upload or replace a revision with `POST/PUT /strategies/modules` and the desired `tag` (e.g., `v1.1.0`). Set `promoteLatest: true` to move `latest` to that hash in one step.
2. To add additional aliases (e.g., `stable`), include them in the `aliases` array for the same upload.
3. To repoint `latest` without changing code, re-upload the existing source with `promoteLatest: true` and the desired `tag`, or manually update `registry.json` via the bootstrap script.
4. To roll back, upload the previously hashed revision (or use `name@hash` when creating instances) and refresh. Running instances pinned to a hash will stay on that revision until manually refreshed.

Remember that deletion is blocked while any instance references the target hash; stop or reconfigure dependent instances before pruning old revisions.

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
| `GET`  | `/strategies/modules` | Return module summaries (hashes, tag aliases, metadata, revision list) plus a `running` block for active hashes. Supports `strategy`, `hash`, `runningOnly`, `limit`, and `offset` query parameters. |
| `POST` | `/strategies/modules` | Create a module. Body accepts `source` plus optional `name`, `filename`, `tag`, `aliases`, `promoteLatest`. Validates compilation, writes to `registry.json`. |
| `GET`  | `/strategies/modules/{name}` | Fetch metadata/file info for a module by strategy name. |
| `GET`  | `/strategies/modules/{name}/source` | Return raw JS source (`application/javascript`). Accepts canonical name or filename. |
| `PUT`  | `/strategies/modules/{selector}` | Replace strategy source. Same payload as POST (`selector` may be name, `name:tag`, or `name@hash`). |
| `DELETE` | `/strategies/modules/{selector}` | Remove a module or revision. Deletion is refused if the hash is referenced by any running instance. |
| `GET`  | `/strategies/modules/{selector}/usage` | Resolve the selector and return revision usage (`count`, `instances`, `firstSeen`, `lastSeen`) plus paginated instance summaries. Supports `limit`, `offset`, and `includeStopped`. |
| `POST` | `/strategies/refresh` | Reload modules from disk and restart affected lambdas. Accepts optional `{"hashes": [], "strategies": []}` payload for targeted refresh. |
| `GET`  | `/strategies/registry` | Export the current `registry.json` merged with live usage counters for auditing or external tooling. |

### Revision Usage Index & Metrics

- The manager maintains a per-revision usage index keyed by `{strategy, hash}`. Each entry tracks running instance IDs, counts, and `firstSeen`/`lastSeen` timestamps. Instance summaries now include this metadata alongside HATEOAS links back to the module usage endpoint.
- Module listings (`GET /strategies/modules`) expose a `running` block for active hashes and accept filters (`strategy`, `hash`, `runningOnly`) plus pagination so operators can focus on specific subsets.
- Drill-down via `GET /strategies/modules/{selector}/usage` to inspect a single revision, optionally including stopped instances.
- Export the full manifest together with usage counters via `GET /strategies/registry` for audits or scripting.
- Prometheus metrics:
  - `strategy_revision_instances` (`{strategy, hash}` gauge) reflects live instance counts.
  - `strategy_revision_instances_total` (`{strategy, hash, action}` counter) logs lifecycle transitions (`start`, `stop`).
- The bootstrap helper supports `--usage usage.json` to highlight revisions with zero usage before pruning.

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

   Upload requests now return HTTP `422 Unprocessable Entity` when compilation, execution, or metadata validation fails. The JSON payload includes a `diagnostics` array detailing the failing stage, message, and optional `line`/`column` and `hint` so authors can iterate quickly without digging through gateway logs.

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

- **Configuration**: `config.app.strategies.directory` points to the strategy tree. Set `config.app.strategies.requireRegistry: true` once migration is complete to reject legacy flat layouts.
- **Deployment**: The loader reads files at startup. File changes require an explicit refresh (HTTP `POST /strategies/refresh` or calling `Manager.RefreshJavaScriptStrategies`). Selectors continue to resolve to the hashes they pinned at launch until refresh.
- **Migration helper**: Run `go run ./scripts/bootstrap_strategies -root <path> -write` to restructure a legacy flat directory into `<name>/<tag>/<name>.js` and generate `registry.json`.
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
- **No automated tag promotion tooling** beyond upload/delete flows—use the REST API or the bootstrap script to manage aliases (e.g., reassigning `latest`).

If you need capabilities outside this surface (e.g., database CRUD, REST calls), expose them as Go helper functions in the bridge—take care to keep them deterministic and to manage blocking semantics so that strategy handlers remain responsive.
