# Strategy Versioning Frontend Integration Guide

## Overview

The backend now manages JavaScript strategies as **versioned modules**. Strategy selectors (`name`, `name:tag`, `name@hash`) resolve to immutable hashes that are stored in `registry.json`. The HTTP surface exposes richer metadata and new safety checks. Update the UI to surface these controls and guide operators through tag management.

## API Changes

### `/strategies/modules` (GET)

- Each module summary now includes:
  - `tagAliases`: map of tag → hash (e.g., `{ "v1.1.0": "sha256:..." }`).
  - `revisions`: array of `{ hash, tag, path, version, size }` describing every known revision.
- **UI action:** render tag/hash columns and a revision timeline/table so operators can inspect available versions.

### `/strategies/modules` (POST) and `/strategies/modules/{selector}` (PUT)

Payload additions:

```json
{
  "source": "<module code>",          // required
  "name": "grid",                     // optional hint when filename omitted
  "filename": "grid.js",              // optional; derived from name/version otherwise
  "tag": "v1.1.0",                    // optional. Adds/updates this tag
  "aliases": ["stable"],              // optional extra tag names -> hash
  "promoteLatest": true                // optional. Move latest -> this hash
}
```

Responses include a `module` object with the resolved hash/tag metadata so the UI can refresh without re-fetching.

### `/strategies/modules/{selector}` (DELETE)

- Selector accepts `name`, `name:tag`, or `name@hash`.
- Backend rejects deletion (409 Conflict) if any instance still pins the hash. Surface the error to the user and suggest stopping/redeploying the instance first.

### Strategy instance data (`/strategy/instances`, `/strategy/instances/{id}`)

Instances now report:

- `strategySelector` (canonical selector used to launch)
- `strategyHash` (pinned hash)
- `strategyTag` (tag resolved at launch, if any)

Use these fields to show whether an instance is on the latest revision.

## UI Requirements

1. **Module Catalogue**
   - Display current tag (`latest`) and pinned hash.
   - Show revision history (using `revisions`).
   - Provide actions: *Promote tag*, *Add alias*, *Delete revision* (with confirmation if hash not in use).

2. **Upload / Replace Modal**
   - Inputs for tag and aliases.
   - Checkbox for `Promote latest` (default true for new releases).
   - Source editor as before.

3. **Instance Detail View**
   - Show `strategyHash`, `strategyTag`, and whether it matches `tagAliases.latest`.
   - Offer hint or CTA to refresh/redeploy when drift detected.

4. **Error Handling**
   - Map 409 responses on delete to a friendly message: “Revision is pinned by running instances. Stop or redeploy them before deleting.”

5. **Selector Inputs**
   - Wherever the UI accepts a strategy identifier, allow `name`, `name:tag`, or `name@hash`.

## Sample Requests/Responses

### Upload New Revision

```http
POST /strategies/modules
Content-Type: application/json

{
  "name": "grid",
  "tag": "v1.1.0",
  "aliases": ["stable"],
  "promoteLatest": true,
  "source": "module.exports = { ... };"
}
```

Response:

```json
{
  "status": "pending_refresh",
  "strategyDirectory": "strategies",
  "module": {
    "name": "grid",
    "hash": "sha256:abc123...",
    "version": "v1.1.0",
    "tagAliases": {
      "v1.1.0": "sha256:abc123...",
      "latest": "sha256:abc123...",
      "stable": "sha256:abc123..."
    },
    "revisions": [
      { "hash": "sha256:abc123...", "tag": "v1.1.0", "version": "v1.1.0", "path": "grid/v1.1.0/grid.js", "size": 2048 },
      { "hash": "sha256:def456...", "tag": "v1.0.0", "version": "v1.0.0", "path": "grid/v1.0.0/grid.js", "size": 1996 }
    ]
  }
}
```

### Delete Revision (when in use)

```http
DELETE /strategies/modules/grid@sha256:abc123...
```

Response:

```json
{ "error": "strategy revision sha256:abc123 is in use" }
```

Display this verbatim and guide the operator to stop/redeploy instances first.

## Rollout Tips

- Refresh module list immediately after uploads using the returned `module` data.
- For large registries, offer filtering by tag/hash.
- Consider adding a “compare revisions” view to highlight code diffs between hashes (optional stretch goal).
- Coordinate with backend release: ensure the gateway with versioned APIs is deployed before enabling the new UI.

For more backend details, see `docs/js-strategy-runtime.md` and `docs/strategy-versioning-plan.md`.
