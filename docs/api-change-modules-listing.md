## API Change Brief – Docker-Style Strategy Module Listing

### 1. Overview

The `/strategies/modules` family of endpoints will behave like `docker images`: they expose registry metadata only. Runtime usage data (instances, counts, selectors) is no longer embedded in listing/detail payloads. Operators now consult `/strategies/modules/{selector}/usage` (or attempt the action and inspect the error payload) when they need live usage information.

### 2. Breaking Changes

1. **`running` array removed** – `StrategyModuleSummary` no longer includes the `running` field. The OpenAPI schema drops `running` from `StrategyModuleSummary` and removes the nested `ModuleRunningSummary` references from listing responses.
2. **`runningOnly` query deprecated** – `GET /strategies/modules?runningOnly=true` previously filtered by active instances; the parameter is removed. Requests that include it return `400 invalid query` to catch stale clients.
3. **Delete/tag error payloads** – Since listings no longer include usage, destructive operations (`DELETE /strategies/modules/{selector}`, tag PUT/DELETE) now include a `usage` snapshot in their error payload when the operation is blocked due to running instances.
4. **UI/CLI impact** – Dashboards that previously relied on inline `running` data must issue explicit usage calls before showing “active” badges.

### 3. Endpoint Details

| Endpoint | Change |
| --- | --- |
| `GET /strategies/modules` | Response items omit `running`. Query parameters drop `runningOnly`. Documentation updated to describe purely registry data (name, file, tags, revisions, metadata). |
| `GET /strategies/modules/{selector}` | Same contract as listing: `running` removed from the summary. Clients fetch `/strategies/modules/{selector}/usage` for instance details. |
| `GET /strategies/modules/{selector}/usage` | Unchanged schema. Clients must rely on this endpoint for runtime insights. |
| `PUT/DELETE /strategies/modules/{name}/tags/{tag}` | On failure due to pinned hashes, the 409 response now embeds usage records so operators see why the request was rejected. |
| `DELETE /strategies/modules/{selector}` | Same as tag routes—conflict responses include usage details. |

### 4. Migration Guidance

1. **Client updates**
   - Remove any parsing logic for `module.running` in listing/detail responses.
   - Stop passing `runningOnly`; expect HTTP 400 if still provided.
   - When you need to display running status, issue `GET /strategies/modules/{selector}/usage` (optionally cached) or rely on error payloads from delete/tag attempts.
2. **UI changes**
   - Replace inline “running instances” badges with either: (a) lazy-loaded usage overlays, or (b) state derived from dedicated usage calls.
   - Update confirmation dialogs for destructive actions to display usage info from the 409 response body when available.
3. **Automation/CLI**
   - Scripts that filtered modules by `runningOnly` should switch to `GET /strategies/modules/{selector}/usage` and inspect the returned `total` count.

### 5. Error Handling

When operations fail due to active instances, responses now include:

```jsonc
{
  "error": "strategy revision in use",
  "details": {
    "selector": "logging@sha256:abcd...",
    "usage": {
      "hash": "sha256:abcd...",
      "instances": ["logging-prod", "logging-drill"],
      "count": 2
    }
  }
}
```

Clients should surface this information in the UI/CLI to guide operators toward `/strategies/modules/{selector}/usage` before retrying.

### 6. Timeline & Validation

- Update OpenAPI (`frontend-api.yaml`) and regen clients **before** deploying the server change.
- Ensure automated tests assert absence of `running` in listing responses and presence of usage data in 409 error payloads.
- Coordinate UI/CLI releases so they no longer depend on the removed fields before enabling the server flag.

With these changes, the strategy-module manager now mirrors Docker’s “images vs. containers” split: listings enumerate stored revisions only, while runtime usage is queried explicitly when needed.
