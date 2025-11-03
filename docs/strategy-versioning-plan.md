# Strategy Versioning Upgrade Plan

## Current State

- All strategies are delivered as Goja JavaScript modules under the configured `strategies/` directory. The loader compiles each file, extracts its metadata, and the lambda manager wires modules into `BaseLambda` instances via helper bridges.
- Strategy code changes only take effect after `/strategies/refresh`; the manager stops each dependent lambda and restarts it with the new module. There is no built-in notion of historical versions—uploading a new file replaces the old one.
- HTTP APIs expose module CRUD (`/strategies/modules`), source retrieval, and refresh. The backtest CLI and tests already consume strategies entirely through the loader layer.

## Upgrade Goals

Introduce Docker-style version semantics:

1. **Human-friendly tags** for everyday operations (e.g., `strategy:v1.1.0`, `strategy:latest`).
2. **Immutable hashes** that pin a running instance to the exact build it launched with.
3. Control-plane support for resolving tags to hashes and reporting which revision is running.

## Proposed Changes

### 1. Metadata Contract

- Extend `module.exports.metadata` to include optional `version` and `hash` fields.
  ```js
  metadata: {
    name: "delay",
    version: "1.1.0",
    hash: "sha256:abc...",
    ...
  }
  ```
- If absent, the loader generates a default version (e.g., `untagged`) and computes the hash from the module source.

### 2. Filesystem Layout & Registry

- Adopt a version-aware layout such as:
  ```
  strategies/
    delay/
      v1.0.0/
        delay.js
      v1.1.0/
        delay.js
  ```
- Maintain a registry file (JSON/YAML) that maps tags to hashes and file paths:
  ```json
  {
    "delay": {
      "tags": {
        "latest": "v1.1.0",
        "v1.1.0": "sha256:abc..."
      },
      "hashes": {
        "sha256:abc...": "v1.1.0"
      }
    }
  }
  ```
- Provide a bootstrap script to reorganize existing modules and produce the initial registry.

### 3. Loader Enhancements

- Update `js.Loader` to read `metadata.version`, compute/validate hashes, and build the registry mapping.
- Expose new helper methods:
  - `ResolveTag(name, tag) -> (path, hash)`
  - `ModuleSummary` returns name, version, hash, and tag list.
- Treat the registry as optional: if missing, fall back to current behavior (single file per strategy, treated as `latest`).

### 4. Runtime Manager Updates

- Allow `LambdaSpec.Strategy.Identifier` to accept:
  - `name` (resolved to default tag)
  - `name:tag`
  - `name@hash`
- Resolve tags to hashes when launching; persist the resolved hash in the spec so running instances stay pinned.
- Modify refresh logic to restart an instance only when the underlying hash changes.
- Surface the resolved version/hash in metadata returned by `StrategyCatalog` and diagnostics.

### 5. HTTP API & CLI Changes

- Extend `/strategies/modules` endpoints:
  - Accept an optional `tag` when uploading or replacing a module.
  - Return version/hash info in responses.
- Allow lambda creation/update APIs to specify tags (`strategy:delay:v1.1.0`).
- Update `cmd/backtest` flags to accept `name:tag` alongside plain names.

### 6. Migration Strategy

- Keep legacy behavior behind a feature flag until the registry is ready.
- Supply a migration guide and tooling to restructure existing modules into versioned directories and populate the registry file.
- Default any unspecified references to `name:latest` to ease adoption.

### 7. Docs & Tests

- Update documentation (`docs/js-strategy-runtime.md`, onboarding guides) with tagging instructions and expected file layout.
- Add unit and integration tests:
  - Tag resolution and fallback behavior.
  - Pinning by hash and ensuring refresh doesn’t roll an instance inadvertently.
  - HTTP CRUD interaction with tags.
- Add CLI/backtest coverage to ensure version arguments resolve correctly.

### 8. Validation & Rollout

- Run `go test ./...` plus any integration suites after each phase.
- Stage the change with real strategy directories to ensure registry parsing, refresh, and pinning work as intended.
- Communicate rollout expectations: how operators tag new builds, promote `latest`, and roll back by selecting older hashes.

## Summary

This plan keeps the existing JS loader pipeline intact while layering Docker-like semantics on top: friendly tags for deployment workflows, content hashes for reproducibility, and registry tooling to keep the two in sync. Once implemented, operators can roll out new strategies via tag promotions, pin running instances to precise hashes, and audit which code version is live across the fleet.
