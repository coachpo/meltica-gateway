## API Change Brief – Registry-Only Strategy Management

### 1. Executive Summary

- The JavaScript strategy loader no longer scans loose `.js` files; it reads `strategies/registry.json` exclusively (previous fallback was `refreshLegacy` in `internal/app/lambda/js/loader.go:210-334`).
- At startup, if `registry.json` is missing, the loader bootstraps an empty `{}` manifest instead of enumerating files.
- Only entries present in `registry.json` are visible or addressable through Control Plane APIs (`GET /strategies/modules`, etc.). Stray files remain on disk but are ignored until registered; `strategies/gc.js` continues to prune untracked files.
- The `config.strategies.requireRegistry` toggle becomes irrelevant—registry enforcement is unconditional (see prior optional guard in `internal/app/lambda/runtime/manager.go:318-343`).

These changes break backward compatibility for deployments that relied on loose-file discovery or skipped updating the manifest.

### 2. Breaking Changes Checklist

1. **Registry mandatory**
   - Gateway start/refresh requires a readable `strategies/registry.json`. Missing file is auto-created; malformed JSON aborts startup.
   - Upload/update/delete/tag APIs no longer succeed if the selector resolves to a file not present in the manifest.

2. **Module catalogue visibility**
   - `/strategies/modules` and `/strategies/modules/{selector}` only surface registered entries; files copied directly into `strategies/` are invisible until the registry references them.
   - Counts (`total`, pagination) reflect registry entries only.

3. **Selector resolution**
   - All selectors (`name`, `name:tag`, `name@hash`) resolve solely via registry data. Previously functional flows that relied on implicit name→file discovery will now return 404/422 errors.

4. **Cleanup responsibility**
   - Control-plane APIs never delete stray files. Operators must run `strategies/gc.js` (already validating registry paths) to remove artifacts not referenced in the manifest.

### 3. Endpoint-Level Changes

| Endpoint | Former Behavior | New Contract |
| --- | --- | --- |
| `GET /strategies/modules` | Returned modules from registry when available or from loose files otherwise. | Returns only registered modules. If registry corrupt/unreadable, responds 500. `strategyDirectory` still points to `config.strategies.directory`, but `modules[]` is empty until registry entries exist. |
| `GET /strategies/modules/{selector}` | Could resolve loose files by name/hash even without registry. | 404 if selector not present in registry, even if matching file exists on disk. Clients must ensure uploads or manual edits register revisions first. |
| `POST /strategies/modules` & `PUT /strategies/modules/{selector}` | Wrote file and inserted/upserted registry entry when registry existed; otherwise succeeded by scanning files. | Always mutates `registry.json`. If manifest missing, loader creates `{}` before write. Any write failure (e.g., permissions) surfaces as 500/422. After upload, refresh still required to load code. |
| `DELETE /strategies/modules/{selector}` | Removed revision by selector; could target loose files. | Only registered hashes/tags can be deleted. Attempting to delete an unregistered file now returns 404/422 `strategy module not found`. Running instances still block deletion. |
| `PUT /strategies/modules/{name}/tags/{tag}` & `DELETE ...` | Operated on registry entries but could run even when registry absent (degenerated to in-memory tags). | Tags exist only in registry. Assign/Delete fails if `registry.json` missing/corrupt; clients should expect 500 until manifest fixed. |
| `GET /strategies/modules/{selector}/source` | Served file if present anywhere. | 404 when selector not registered. Used for editor downloads; ensure registry references new hashes before fetching. |
| `POST /strategies/refresh` | Refresh fell back to scanning directory when registry absent. | Refresh reads registry exclusively. Entries referencing missing files trigger errors; stray files aren’t loaded. |
| `GET /strategies/registry` | Already returned manifest + usage. | Unchanged schema, but now represents the only source of truth. Clients must treat this as equivalent to a Docker registry export. |

### 4. Loader & Runtime Behavior

- Loader initialization path (`internal/app/lambda/js/loader.go`) now follows:
  1. Attempt to read `registry.json`.
  2. If missing, create empty file and proceed with zero modules.
  3. Load modules strictly from registry entries, validating `strategy/<hash>/<name>.js` paths.
  4. Ignore any files not referenced; provide diagnostics/logs for mismatches.
- Runtime manager no longer needs `cfg.Strategies.RequireRegistry`; enforcement is implicit.

### 5. Client Migration Guide

1. **Baseline registry**
   - Export the current registry via `GET /strategies/registry`. If you previously relied on loose files, run internal tooling (or craft a script similar to `internal/testutil/strategies`) to compute hashes and populate `registry.json`.
   - Commit the manifest alongside strategy sources as already recommended in `strategies/README.md`.

2. **CI/CD updates**
   - Ensure all uploads go through `POST /strategies/modules` (or the CLI that uses it) so new revisions immediately land in the manifest.
   - When manually editing `registry.json`, schedule a refresh before expecting instances to see changes.

3. **Selector usage**
   - Audit any automation that referenced modules by filename alone. Update to use canonical selectors (name/tag/hash) that the registry resolves.
   - Watch for 404/422 responses from module/tag APIs; treat them as indicators that the selector is unregistered.

4. **Cleanup workflow**
   - Add `node strategies/gc.js --write` (or equivalent) to ops runbooks after deletions to remove unreferenced directories. The Go control plane will ignore leftovers indefinitely.

5. **Monitoring**
   - Use `GET /strategies/modules?runningOnly=true` and `GET /strategies/registry` to verify that all running hashes have manifest entries.
   - Alert on refresh failures caused by malformed registry entries (e.g., invalid paths), since no auto-fallback exists anymore.

### 6. Reference Implementation Touchpoints

- Loader registry-only requirement replaces legacy scan (`internal/app/lambda/js/loader.go:210-334`, `900-942` for registry I/O).
- Runtime manager no longer needs optional `RequireRegistry` guard (`internal/app/lambda/runtime/manager.go:318-343`).
- All HTTP strategy module handlers (`internal/infra/server/http/server.go:360-980`) now depend on registry-backed resolution exclusively.

### 7. Operational Notes

- Because the loader now ignores stray files, intentionally staging code directly into `strategies/` without updating `registry.json` has no operational impact until the manifest is edited—use this to safely check files in before assigning tags.
- Registry corruption (syntax errors, duplicate hashes, invalid paths) will surface earlier: refresh/startup fails immediately, prompting corrective action instead of silently loading partial data.
- `strategies/gc.js` remains the canonical cleanup utility; scheduling it after tag removals keeps disk usage aligned with the manifest.

Share with frontend/backend integrators so they can adjust API clients, CI pipelines, and operational runbooks ahead of the registry-only rollout.
