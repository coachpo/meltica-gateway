# Backend Strategy Versioning Upgrade Plan

## Overview

As the registry accumulates multiple revisions per strategy, the runtime must expose clearer insight into which hashes are live, support targeted refreshes, and keep revision lookups efficient. This document lays out the backend-only upgrades needed to keep the system usable when many versions run concurrently.

## Goals

- Surface per-revision usage data (counts, instance IDs, timestamps) without scanning the full manifest on every request.
- Provide dedicated APIs for version usage queries so operators can filter, paginate, and drill into specific hashes.
- Preserve low-latency loader lookups even when the registry spans dozens of revisions per strategy.
- Enable targeted lifecycle operations (refresh/restart) for specific hashes without disturbing unrelated instances.
- Supply migration tooling and documentation that reflects the richer version semantics.

## Enhancements by Layer

### Runtime Manager

1. **Revision Usage Index**
   - Maintain an in-memory map keyed by `{strategyName, hash}` that stores running instance IDs, counts, and `firstSeen`/`lastSeen` timestamps.
   - Update the index in `ensureSpec`, `Start`, `Stop`, `RefreshJavaScriptStrategies`, and during spec cloning so the HTTP layer always returns consistent usage data.
   - Include Prometheus gauges/counters for `strategy_revision_instances` to aid drift monitoring.

2. **Spec Cloning & Serialization**
   - Extend `cloneSpec` and JSON responses to include the usage summary so `/strategy/instances` and module endpoints can link directly to hash consumers.

### Loader & Registry

1. **Hash Resolution Optimisation**
   - Cache frequently resolved hashes (LRU) to reduce repeated registry scans when many aliases map to the same digest.
   - Add a `retired` flag in module summaries for hashes with zero usage, allowing pruning scripts to skip active revisions.

2. **Registry Diagnostics**
   - Emit a consolidated dump endpoint (see HTTP section) that combines registry metadata with usage counts.

### HTTP API (`internal/infra/server/http/server.go`)

1. **Module Usage Endpoints**
   - Extend `GET /strategies/modules` to accept optional filters (`strategy`, `hash`, `runningOnly`) and include a `running` block for each module: `{ hash, instances, count, firstSeen, lastSeen }`.
   - Add `GET /strategies/modules/{selector}/usage` for detailed drill-down (auto-resolving selectors using the loader). Support `limit`, `offset`, and `includeStopped` parameters.

2. **Targeted Refresh**
   - Introduce `POST /strategies/refresh` payload variant `{ hashes: [], strategies: [] }` to refresh specific revisions without reloading the whole catalog.
   - Update responses to return reason codes (`refreshed`, `alreadyPinned`, `retired`) so UIs can display actionable feedback.

3. **Instance Responses**
   - Embed hyperlinks (via HATEOAS-style URLs) back to the usage endpoints for quick navigation from instance lists to module usage summaries.

### Lifecycle Controls

1. **Restart Guardrails**
   - In `RefreshJavaScriptStrategies`, verify the resolved hash changed before restarting and accumulate reasons for no-op restarts.

2. **Batch Operations**
   - Provide a helper in `runtime.Manager` to refresh/restart all instances pinned to a given hash, enabling orchestrated rollbacks or promoted rollouts.

## Tooling & Migration

1. **Registry Export**
   - Add `GET /strategies/registry` to return the raw registry plus usage counters for audit, CSV export, or scripting.

2. **Bootstrap Updates**
   - Enhance `scripts/bootstrap_strategies` with a `--usage` flag that merges exported usage data into the generated registry, highlighting unused revisions for pruning.

3. **Documentation**
   - Update `docs/strategy-versioning-plan.md` to describe the new APIs, usage metrics, and recommended operational workflows (promoting versions, scanning for drift, retiring hashes).

## Validation & Rollout

1. **Testing**
   - Unit tests: loader hash cache, usage index mutations, and new HTTP handlers.
   - Integration tests: create multiple instances across several hashes, exercise the usage endpoints, and verify targeted refresh semantics.
   - Performance baselines: measure `/strategies/modules` latency with large registries before/after hash caching.

2. **Staged Deployment**
   - Deploy to staging with synthetic loads that create >50 revisions per strategy.
   - Verify dashboards (new Prometheus metrics) and run bootstrap tooling against exported registry snapshots.

3. **Communication**
   - Share release notes covering API additions, migration steps, and dashboard updates.
   - Provide sample queries (curl/Postman) demonstrating how to filter usage by hash/tag.

## Open Questions

- How much history should the usage index retain (e.g., should `lastSeen` persist across restarts via disk snapshot)?
- Do we need rate limits on the drill-down endpoint to avoid returning thousands of instances in a single response?
- Should retired revisions be marked automatically after a configurable TTL with zero usage, or remain manual?

These decisions can be refined during implementation based on operational requirements.
