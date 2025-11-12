# JavaScript Strategy Operations Manual

This manual explains how to onboard, run, and maintain JavaScript trading strategies inside the Meltica gateway. It distills the developer notes in `docs/devnotes/js-strategy-runtime.md` and `docs/devnotes/strategy-versioning-plan.md` into a user-facing guide for operators and strategy authors.

---

## 1. Core Concepts

- **Strategy home**: Every revision lives under `strategies/<name>/<hash>/<name>.js` (hash = 64-character SHA-256 digest). `strategies/registry.json` is the source of truth linking tags/aliases to hashes and file paths, so tags never dictate the directory layout.
- **Execution engine**: The gateway loads strategies through the Goja JavaScript runtime. Each module is wrapped in Go code (`internal/app/lambda/js/*`) that exposes helper functions for logging, timing, provider selection, market data, and order submission.
- **Selectors**: Anywhere you reference a strategy you can use:
  - `name` → whatever hash the registry maps to `tags.latest`.
  - `name:tag` → an explicit tag such as `v1.0.0`.
  - `name@hash` → an immutable SHA-256 hash.
- **Registry authority**: The loader recalculates hashes on refresh and ensures tags/aliases point to the correct revision. Deleting a revision is blocked while any running instance still pins that hash.

---

## 2. Life Cycle at a Glance

1. **Startup**
   - `runtime.NewManager` builds a loader pointed at `config.app.strategies.directory` (defaults to `strategies`).
   - `loader.Refresh` compiles every module, reads metadata, and registers it in memory.
2. **Launch**
   - A lambda or API request resolves the selector to a hash, invokes `js.NewStrategy`, wires helpers via `Attach`, and starts the lambda.
3. **Refresh**
   - Call `POST /strategies/refresh` (or `Manager.RefreshJavaScriptStrategies`). Optional payload `{"hashes": [], "strategies": []}` targets specific revisions.
   - Response includes reason codes per selector: `refreshed`, `alreadyPinned`, `retired`.
4. **Stop/Shutdown**
   - `Manager.Stop(id)` cancels the strategy context, unregisters HTTP routes, and calls the strategy’s `Close` method to tear down the Goja VM.

Running instances always pin a hash; they only restart when refresh detects that the underlying source changed.

---

## 3. Creating or Updating a Strategy

1. **Author the module**

   ```js
   module.exports = {
     metadata: {
       name: "my-strategy",
       tag: "v1.2.0",
       displayName: "My Strategy",
       description: "Short blurb for operators",
       config: [{ name: "threshold", type: "float", default: 0.5 }],
       events: ["Trade", "ExecReport"],
     },
     create(env) {
       const log = (...args) => env.helpers.log("[MYSTRAT]", ...args);
       const { runtime } = env;
       return {
         wantsCrossProviderEvents: () => false,
         onTrade(ctx, evt) {
           if (!runtime.isTradingActive()) return;
           const provider = runtime.selectProvider(Date.now());
           log("selected provider", provider.name);
         },
       };
     },
   };
   ```

- `metadata.tag` is required for registry writes; keep it semver-like (`vMAJOR.MINOR.PATCH`) so operators can reason about rollouts. Treat metadata tags as build IDs—use the tag APIs (`reassignTags` or `PUT /strategies/modules/{name}/tags/{tag}`) to move higher-level aliases such as `prod`/`latest`.
   - Keep logic deterministic—long blocking calls inside JS pause the Goja goroutine.
   - Use injected helpers for logging, sleeps, provider selection, market state, and order submission.

2. **Register the revision**

   - Upload via `POST /strategies/modules` with `source`, `tag`, optional `aliases`, and `promoteLatest`. The control plane copies the supplied tag into module metadata, so keep them synchronized.
   - Or drop the file into the directory and run `POST /strategies/refresh`.
   - Validation failures return HTTP `422` with a `diagnostics` array (stage, message, line/column, hint).

3. **Launch or backtest**

   - Reference the strategy by name/tag/hash in lambda manifests, CLI invocations, or the backtest runner (`cmd/backtest -strategy name:tag`).

4. **Validate**
   - Run `make test` to exercise the JS pipeline end-to-end.
   - Optional: run `make backtest STRATEGY=name` for offline evaluation.

---

## 4. Operating the Catalogue (HTTP Surface)

| Method   | Path                                   | Purpose                                                                                                                            |
| -------- | -------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `GET`    | `/strategies`                          | In-memory metadata overview.                                                                                                       |
| `GET`    | `/strategies/{name}`                   | Single strategy metadata.                                                                                                          |
| `GET`    | `/strategies/modules`                  | Module catalogue with hashes, tags, aliases, and `running` block (filter by `strategy`, `hash`, `runningOnly`, `limit`, `offset`). |
| `POST`   | `/strategies/modules`                  | Create a module; validates compilation before writing. Supports `reassignTags` to move aliases immediately after upload.           |
| `GET`    | `/strategies/modules/{name}`           | Metadata/file info, resolved by name.                                                                                              |
| `GET`    | `/strategies/modules/{name}/source`    | Raw JS source.                                                                                                                     |
| `PUT`    | `/strategies/modules/{selector}`       | Replace an existing revision (selector can be name, `name:tag`, or `name@hash`).                                                   |
| `DELETE` | `/strategies/modules/{selector}`       | Delete a revision (blocked while any instance references the hash).                                                                |
| `PUT`    | `/strategies/modules/{name}/tags/{tag}`| Reassign a tag to a new hash. Body: `{ "hash": "sha256:…", "refresh": true|false }`.                                           |
| `DELETE` | `/strategies/modules/{name}/tags/{tag}`| Remove a tag alias (`?allowOrphan=true` bypasses the guard that protects the last selector for a hash).                          |
| `GET`    | `/strategies/modules/{selector}/usage` | Revision usage counters with paginated instances; `includeStopped=true` shows dormant pins.                                        |
| `POST`   | `/strategies/refresh`                  | Reload modules from disk, optionally targeting specific hashes/strategies.                                                         |
| `GET`    | `/strategies/registry`                 | Export `registry.json` merged with live usage counters for tooling/dashboards.                                                     |

### Usage Index & Metrics

- The runtime keeps a `{strategy, hash}` usage index with `count`, `instances`, `firstSeen`, `lastSeen`.
- Prometheus metrics:
  - `strategy_revision_instances` gauge tracks live instance counts.
  - `strategy_revision_instances_total` counter (labels `start`/`stop`) audits churn.
  - `strategy_tag_reassigned_total` counter (labels `environment`, `strategy`, `tag`) counts alias moves.
  - `strategy_tag_deleted_total` counter (labels `environment`, `strategy`, `tag`, `allowOrphan`) records alias removals.

---

## 5. Tagging & Promotion Workflows

### Tag Semantics & Guard Rails

- Tags behave like Docker image tags: each alias maps to exactly one hash, and hashes remain callable via `name@sha256:…` forever.
- Moving a tag—either inline via `POST /strategies/modules` (`reassignTags`) or later via `PUT /strategies/modules/{name}/tags/{tag}`—only changes the pointer; the previous revision stays on disk and in the registry.
- Deleting a tag removes the alias, not the revision. Use `DELETE /strategies/modules/{name}/tags/{tag}` with `allowOrphan=true` only when you intentionally want to drop the last selector for a hash.
- `latest` is immutable: reassign it instead of deleting it.
- Every tag move/delete is logged by the manager and exported to telemetry so dashboards/audits can flag unexpected churn.
- The helper script `scripts/strategy-tags.sh` wraps the HTTP APIs with Docker-style prompts (`MELTICA_API` selects the control plane, `REFRESH=false` skips automatic refreshes, `ALLOW_ORPHAN=true` forces deletions).

### Promoting a New Revision

1. `POST /strategies/modules` with the new source, `tag`, optional `aliases`, and `reassignTags` (set `promoteLatest: true` if this should become the default). `reassignTags: ["latest","prod"]` atomically moves those aliases to the uploaded hash.
2. Verify via `GET /strategies/modules?strategy=name&hash=sha256:...`.
3. `POST /strategies/refresh { "strategies": ["name:tag"] }` to roll only affected instances.
4. Watch `strategy_revision_instances` to confirm the fleet runs the new hash.

### Detecting Drift

1. `GET /strategies/modules?runningOnly=true` to list hashes currently serving traffic.
2. Drill into suspicious selectors using `GET /strategies/modules/{selector}/usage` (`includeStopped=true` helps find dormant pins).
3. Periodically export `GET /strategies/registry` and diff it to catch unexpected tag → hash changes.

### Retiring Old Hashes

1. Export the control-plane usage report via `GET /strategies/registry` and store it for auditing.
2. Stop any lingering instances referencing hashes you intend to retire.
3. Move any aliases off the hash (if you skipped `reassignTags` earlier) and `DELETE /strategies/modules/{name@hash}` once no instances reference it.
4. Rebuild the local `registry.json` after the deletions so dashboards stay in sync.

---

## 6. Tooling & Tests

- **Backtest CLI**: `make backtest STRATEGY=name[:tag]` runs strategies offline using CSV data via `backtest.NewEngine`.
- **CI expectations**: `make lint`, `make test`, and `make coverage` (≥ 70%) must pass before merging strategy updates.

---

## 7. Troubleshooting Checklist

- **Strategy missing from catalogue?** Ensure the file resides under `strategies/<name>/<tag>/<name>.js`, the registry entry exists, and run `POST /strategies/refresh`.
- **Upload rejected with 422?** Inspect the `diagnostics` array for compilation/metadata errors; fix locally and re-upload.
- **Refresh returns `alreadyPinned`?** The running instance already points at that hash. Launch a new instance or upload a new revision to trigger a change.
- **Delete blocked?** Stop or reconfigure instances still referencing the hash (`GET /strategies/modules/{selector}/usage` reveals them).
- **Strategy stuck after code change?** Confirm you refreshed the directory and that the instance selector isn’t pinned to an old hash.

Stay disciplined about tagging, refreshing, and pruning. A healthy registry plus the telemetry mentioned above keeps the JavaScript strategy fleet predictable and auditable.
