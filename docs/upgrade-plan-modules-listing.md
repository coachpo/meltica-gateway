## Upgrade Plan – Docker-Style Strategy Module Manager

### Goal
Make `/strategies/modules` behave like `docker images`: expose only registry metadata, no inline runtime usage. Usage insights move to `/strategies/modules/{selector}/usage` or conflict responses.

### 1. Code Changes

1. **API Schema**
   - Update `frontend-api.yaml`:
     - Remove `running` from `StrategyModuleSummary`.
     - Delete references to `ModuleRunningSummary` under module listings.
     - Remove the `runningOnly` query parameter definition (and document 400 if present).
     - Add error-schema examples showing usage data in 409 responses for delete/tag routes.
2. **HTTP Handlers (`internal/infra/server/http/server.go`)**
   - `listStrategyModules`: stop copying `ModuleSummary.Running` into the response. Instead, zero it out before serialization (or adjust `ModuleSummary` to omit the field entirely for API responses).
   - Remove support for the `runningOnly` query param; reject it with `400 invalid query`.
   - `getStrategyModule`: same change—strip the `Running` slice from the payload.
   - `deleteStrategyModule` / `assignStrategyModuleTag` / `deleteStrategyModuleTag`: when `manager.RemoveStrategy/AssignStrategyTag/DeleteStrategyTag` returns “in use” errors, wrap the response with `RevisionUsageSummary` data so the API matches the new error payload contract.
3. **Loader / Manager**
   - No functional change required for data sources, but add helpers to fetch usage snapshots when an operation fails (so HTTP handlers don’t re-resolve selectors twice).
4. **Structs / JSON**
   - Introduce a dedicated response struct for `StrategyModuleSummary` that omits `Running`, so internal Go structs can still carry usage info for other subsystems (websocket dashboards, telemetry) without leaking it via the REST API.
5. **Query Validation**
   - Add a shared helper to validate module listing query params, returning `400` if unknown params (like `runningOnly`) are supplied—helps catch stale clients early.

### 2. Testing

1. **Unit tests**
   - `internal/infra/server/http/server_test.go`:
     - Update `TestFilterModuleSummaries` expectations (no usage filtering, no `running` field).
     - Add tests verifying `runningOnly` yields HTTP 400.
     - Add delete/tag tests asserting usage payloads in 409 responses.
   - `frontend-api.yaml` schema tests (if applicable) or contract tests verifying regenerated clients no longer see `running`.
2. **Integration**
   - End-to-end API tests that compare pre/post responses for `/strategies/modules` and confirm `running` is absent while `/strategies/modules/{selector}/usage` remains intact.

### 3. Client / UI Coordination

1. Regenerate API clients after updating the OpenAPI spec.
2. Update dashboards/CLI:
   - Remove reliance on `module.running`.
   - Add explicit calls to `/strategies/modules/{selector}/usage` when showing “running” badges.
   - Handle new 400 error for `runningOnly`.
3. Communicate the new error payload contract so destructive-action dialogs can show blockers from the server.

### 4. Deployment Sequence

1. Land server + spec changes behind a feature flag that strips `running` but still allows legacy behavior if rollback is needed (since backward compatibility is not required, the flag can be short-lived).
2. Release updated clients/UIs once regenerated.
3. Remove the feature flag and deploy broadly.

Following this plan ensures the codebase, API spec, and clients transition together to the Docker-like module listing model.
