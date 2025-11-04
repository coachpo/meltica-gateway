# Strategy Versioning Operations

This guide documents the operational surface for managing JavaScript strategy revisions after the runtime upgrade. It complements the deeper loader/runtime walkthrough in `docs/js-strategy-runtime.md` and focuses on day-to-day tasks for operators.

---

## Key APIs

- **Module catalogue** – `GET /strategies/modules` now returns revision metadata, tag aliases, and a `running` block that lists hashes with active instances. Use the `strategy`, `hash`, `runningOnly`, `limit`, and `offset` parameters to focus on a subset of revisions.
- **Revision drill‑down** – `GET /strategies/modules/{selector}/usage` resolves a selector (`name`, `name:tag`, or `name@hash`) and returns usage counters (`count`, `instances`, `firstSeen`, `lastSeen`) plus paginated instance summaries. Pass `includeStopped=true` to review dormant instances that still pin an old hash.
- **Targeted refresh** – `POST /strategies/refresh` accepts an optional payload `{ "hashes": [], "strategies": [] }`. Each entry in the response carries a reason code (`refreshed`, `alreadyPinned`, `retired`) so operators know exactly which instances restarted.
- **Registry export** – `GET /strategies/registry` emits the raw `registry.json` merged with live usage counters. Downstream tools (reports, dashboards, scripts) can consume this snapshot without touching the gateway filesystem.

---

## Operational Workflows

### Promoting a New Revision

1. Upload or update the module via `POST/PUT /strategies/modules` with the desired `tag` and optional aliases.
2. Verify the revision appears in `GET /strategies/modules` (filter by `hash` or `strategy`).
3. Run a targeted refresh with `POST /strategies/refresh { "strategies": ["name:tag"] }`. Confirm the response reports `refreshed` for the expected instances.
4. Monitor `strategy_revision_instances` in Prometheus to ensure the new hash reaches the intended fleet size.

### Scanning for Drift

1. Fetch `GET /strategies/modules?runningOnly=true` to list hashes that currently have traffic.
2. Drill into suspicious revisions using `GET /strategies/modules/{selector}/usage` to review the instance roster (running vs. stopped).
3. Export `GET /strategies/registry` on a cadence (or feed it into a diffing job) to detect tags pointing at unexpected hashes.

### Retiring Unused Hashes

1. Export usage data: `curl …/strategies/registry > usage.json` (or replay from automation).
2. Run `go run ./scripts/bootstrap_strategies --usage usage.json` to highlight revisions with zero running instances.
3. For hashes that are safe to prune, stop remaining instances if any, then `DELETE /strategies/modules/{name@hash}`.
4. Regenerate the registry (if desired) with `bootstrap_strategies -write` to clean up on disk.

---

## Metrics & Telemetry

- `strategy_revision_instances` – observable gauge with `{strategy, hash}` labels reporting the active instance count.
- `strategy_revision_instances_total` – counter with `{strategy, hash, action}` labels (`start`, `stop`) to audit lifecycle churn.
- Complementary dashboards should join these metrics with module metadata (from the registry export) to surface coverage, drift, and unused revisions.

---

## Tooling Recap

- **Bootstrap script** – `go run ./scripts/bootstrap_strategies -root <dir> [-write] [--usage usage.json]` reorganizes legacy layouts, produces `registry.json`, and (with `--usage`) prints unused revisions.
- **Instance listings** – `/strategy/instances` and `/strategy/instances/{id}` embed revision usage data and navigation links to module usage endpoints, making it easy to pivot from a lambda to the revision it pins.

Keep these workflows in rotation—promote via targeted refresh, routinely scan for drift, and prune unused hashes—to keep the strategy catalogue healthy as the number of revisions grows.

