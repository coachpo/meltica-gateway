# Strategy Usage & Refresh Frontend Integration

## Overview

The gateway now exposes richer runtime metadata for JavaScript strategy revisions. Frontend tooling must surface revision usage, support targeted refresh workflows, and provide direct navigation between instances and their pinned hashes. This document outlines the UI integrations required to consume the new backend contracts delivered in `internal/infra/server/http/server.go`.

## API Reference

### 1. `GET /strategies/modules`

Query parameters:

| Param | Type | Notes |
| --- | --- | --- |
| `strategy` | string | Optional; case-insensitive strategy name filter. |
| `hash` | string | Optional; exact hash selector. Only matching revisions/running entries are returned. |
| `runningOnly` | bool | Optional; when `true`, only modules with active instances are returned. |
| `limit` | int | Optional pagination limit (`>= 0`). |
| `offset` | int | Optional pagination offset (`>= 0`). |

Response payload:

```jsonc
{
  "modules": [
    {
      "name": "grid",
      "hash": "sha256:abc...",
      "version": "v2.1.0",
      "tagAliases": { "latest": "sha256:abc...", "stable": "sha256:abc..." },
      "revisions": [
        {
          "hash": "sha256:abc...",
          "tag": "v2.1.0",
          "path": "grid/v2.1.0/grid.js",
          "version": "v2.1.0",
          "size": 2850,
          "retired": false
        }
      ],
      "running": [
        {
          "hash": "sha256:abc...",
          "count": 4,
          "instances": ["grid-eu-1", "grid-us-2"],
          "firstSeen": "2024-06-01T12:00:00Z",
          "lastSeen": "2024-06-02T09:41:05Z"
        }
      ]
    }
  ],
  "total": 4,
  "offset": 0,
  "limit": 20
}
```

Use the `running` block to render active hash badges and provide quick access to the drill-down view below.

### 2. `GET /strategies/modules/{selector}/usage`

Selector accepts `name`, `name:tag`, or `name@hash`. Query parameters:

| Param | Type | Notes |
| --- | --- | --- |
| `limit` | int | Optional pagination limit (`>= 0`). |
| `offset` | int | Optional pagination offset (`>= 0`). |
| `includeStopped` | bool | When `true`, include instances that exist but are not running. |

Response structure:

```jsonc
{
  "selector": "grid@sha256:abc...",
  "strategy": "grid",
  "hash": "sha256:abc...",
  "usage": {
    "strategy": "grid",
    "hash": "sha256:abc...",
    "count": 4,
    "instances": ["grid-eu-1", "grid-us-2"],
    "firstSeen": "2024-06-01T12:00:00Z",
    "lastSeen": "2024-06-02T09:41:05Z",
    "running": true
  },
  "instances": [
    {
      "id": "grid-eu-1",
      "strategyIdentifier": "grid",
      "strategyHash": "sha256:abc...",
      "strategyTag": "v2.1.0",
      "running": true,
      "usage": {
        "strategy": "grid",
        "hash": "sha256:abc...",
        "count": 4,
        "firstSeen": "2024-06-01T12:00:00Z",
        "lastSeen": "2024-06-02T09:41:05Z",
        "running": true
      },
      "links": {
        "self": "/strategy/instances/grid-eu-1",
        "usage": "/strategies/modules/grid@sha256:abc.../usage"
      }
    }
  ],
  "total": 4,
  "offset": 0,
  "limit": 25
}
```

Leverage the `links.usage` pointer to maintain consistent navigation loops between usage panels and instance views.

### 3. `POST /strategies/refresh`

- **Without payload**: refreshes the full catalogue; response `{ "status": "refreshed" }`.
- **With payload**:

```jsonc
{
  "hashes": ["sha256:abc..."],
  "strategies": ["grid:canary", "delay@sha256:def..."]
}
```

Response example:

```jsonc
{
  "status": "partial_refresh",
  "results": [
    {
      "selector": "grid:canary",
      "strategy": "grid",
      "hash": "sha256:abc...",
      "previousHash": "sha256:old...",
      "instances": ["grid-eu-1", "grid-us-2"],
      "reason": "refreshed"
    },
    {
      "selector": "delay@sha256:def...",
      "strategy": "delay",
      "hash": "sha256:def...",
      "previousHash": "sha256:def...",
      "instances": ["delay-apac"],
      "reason": "alreadyPinned"
    },
    {
      "selector": "sha256:zzz...",
      "hash": "sha256:zzz...",
      "reason": "retired"
    }
  ]
}
```

Reason codes:

| Code | Meaning |
| --- | --- |
| `refreshed` | The hash changed; instance restarted. |
| `alreadyPinned` | The instance already ran the requested hash. |
| `retired` | The selector/hash could not be resolved (module missing or no longer active). |

Surface these codes directly in the UI so operators understand the outcome.

### 4. `GET /strategies/registry`

Returns the on-disk `registry.json` merged with usage counts:

```jsonc
{
  "registry": {
    "grid": {
      "tags": { "latest": "sha256:abc...", "v2.1.0": "sha256:abc..." },
      "hashes": {
        "sha256:abc...": { "tag": "v2.1.0", "path": "grid/v2.1.0/grid.js" }
      }
    }
  },
  "usage": [
    {
      "strategy": "grid",
      "hash": "sha256:abc...",
      "count": 4,
      "instances": ["grid-eu-1"]
    }
  ]
}
```

Use this endpoint for export/download workflows (CSV, audit logs, etc.).

### 5. Instance payloads

- `GET /strategy/instances` now returns `links.self` and `links.usage` for each instance.
- `usage` field mirrors the revision summary, even when `running = false` for stopped instances (useful for drift detection).

Example snippet:

```jsonc
{
  "instances": [
    {
      "id": "grid-eu-1",
      "strategyHash": "sha256:abc...",
      "running": true,
      "links": {
        "self": "/strategy/instances/grid-eu-1",
        "usage": "/strategies/modules/grid@sha256:abc.../usage"
      }
    }
  ]
}
```

## Frontend Integration Tasks

1. **Module Catalogue Enhancements**
   - Add filter controls bound to `strategy`, `hash`, and `runningOnly`.
   - Render the `running` array as badges (count + tooltip for instance IDs).
   - Mark rows with `revisions[].retired = true` to flag unused hashes.

2. **Usage Drill-down View**
   - Implement a dedicated panel/page for `GET /strategies/modules/{selector}/usage`.
   - Paginate instance lists via `limit/offset`. Show stopped instances only when toggled on.
   - Provide quick actions to jump from an instance back to its detail view (use `links.self`).

3. **Targeted Refresh Panel**
   - Build a modal/side sheet that accepts selectors or hashes.
   - POST to `/strategies/refresh` with the selected values and present reason codes per row.
   - Offer “Refresh all” fallback (empty payload) with appropriate confirmation.

4. **Registry Export Integration**
   - Expose a “Download registry” button that fetches `/strategies/registry` and transforms it into CSV/JSON for operators.
   - Optionally display usage counts inline (e.g., highlight hashes with `count = 0`).

5. **Instance Detail Updates**
   - Surface `usage.count`, `usage.firstSeen`, and `usage.lastSeen` inside the instance drawer.
   - Provide a deep link to the usage drill-down via `links.usage`.

## Error Handling & Edge Cases

- Handle `400` validation errors (invalid limit/offset) with inline form messaging.
- Map `409` responses from revision delete attempts (unchanged behavior) to the existing conflict UI.
- When `/strategies/modules/{selector}/usage` returns `total = 0`, present an empty state explaining the selector cannot be resolved (likely retired).
- For targeted refresh results with `reason = retired`, prompt the operator to import the module or unschedule the selector.

## QA Checklist

- [ ] Verify filters and pagination perform the expected query mutations.
- [ ] Confirm `running` counts match the sum of instances returned by the drill-down endpoint.
- [ ] Ensure targeted refresh responses render each reason code accurately.
- [ ] Validate links between module usage, instance list, and instance detail views.
- [ ] Smoke test download/export functionality for large registries (mock with >50 revisions).

## References

- Backend implementation: `internal/infra/server/http/server.go`
- Runtime usage index: `internal/app/lambda/runtime/manager.go`
- Loader usage summaries: `internal/app/lambda/js/loader.go`
- End-to-end plan: `docs/strategy-versioning-plan.md`

