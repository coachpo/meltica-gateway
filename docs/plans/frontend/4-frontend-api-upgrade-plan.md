# Frontend API Upgrade Plan

This document is a hand-off guide for the frontend developer responsible for modernizing the Meltica control UI so it fully leverages the backend control-plane APIs. It covers current coverage, technical debt, target architecture, feature deliveries, and a phased execution plan with practical next steps.

---

## 1. Purpose & Goals
- **Adopt all documented backend APIs** from `docs/frontend-api.md`, eliminating manual command-line workflows for operators.
- **Stabilize the data layer** with typed transports, schema validation, caching, and consistent error semantics.
- **Reduce duplicated logic** across pages by introducing shared hooks and modular API clients.
- **Enable rapid iteration** via testable hooks, MSW-backed fixtures, and clear documentation.

Success metrics:
1. Every page uses the shared data layer; no raw `fetch` calls or ad-hoc query-string building in components.
2. Cache-aware API hooks cover strategies, modules, providers, adapters, instances, risk, runtime config, context backup/restore, and outbox.
3. Operators can read AND mutate runtime config, context snapshots, and strategy registry from the UI.
4. Automated tests exist for critical hooks and complex flows (provider CRUD, instance mutate, strategy module lifecycle).

---

## 2. Current State Summary

### 2.1 API Coverage Matrix
| Feature area | Backend endpoints | Frontend usage today | Notes / gaps |
|--------------|------------------|----------------------|--------------|
| Strategies | `GET /strategies`, `GET /strategies/{name}` | Only list view uses `/strategies`; detail route unused | No detail surface, no metadata refresh |
| Strategy modules | `GET/POST/PUT/DELETE /strategies/modules`, `/usage`, `/source`, `/refresh`, `/registry` | CRUD UI exercise core endpoints, but registry export + refresh filters unused | Manual pagination, no usage call linking across pages |
| Providers | `GET/POST/PUT/DELETE /providers`, `/start`, `/stop`, `/balances`, `/providers/{name}` | CRUD page covers core flows; `/balances` unused | Polling implemented manually, duplicates fetch logic |
| Adapters | `GET /adapters`, `GET /adapters/{identifier}` | List uses `/adapters`; detail fetch unused | Adapter-specific schema not surfaced in provider form |
| Instances | `/strategy/instances` CRUD, `/orders`, `/executions` | Instances page covers list/history; `/instances/{id}` GET seldom used, no diffing | Manual `Promise.all` fetch, no caching of reused data |
| Risk limits | `GET/PUT /risk/limits` | Page exists but lacks schema validation and optimistic UX | |
| Runtime config | `GET/PUT/DELETE /config/runtime` | Not implemented | Need diff + revert UI |
| Context backup | `GET/POST /context/backup` | Only GET flow shipped; restore missing | |
| Outbox | `GET/DELETE /outbox` | Unused | Important for debugging events |

### 2.2 Data Flow Snapshot
- `src/lib/api-client.ts` is a 800+ line class exposing every endpoint but with manual response parsing and stringly typed errors.
- Components import `apiClient`, call it inside `useEffect`, and juggle `loading/error/data` booleans.
- Pagination/filter state lives in components (strategy modules, providers), leading to intertwined UI and data responsibilities.

### 2.3 Key Pain Points
1. **Type drift**: Interfaces in `lib/types.ts` were copied from historical docs; backend structs have evolved (e.g., new risk fields, additional module metadata).
2. **Redundant fetches**: e.g., Instances page fetches strategies/providers/modules every time the dialog closes.
3. **Limited error handling**: All errors (network, 409 conflict, validation) reduce to `Error(message)`; UI cannot act on status codes.
4. **Missing features**: runtime config editor, context restore, outbox viewer, adapter detail, provider balances, strategy usage crosslinks.
5. **Testing gap**: No coverage for API flows; regressions in manual parsing go unnoticed.

---

## 3. Target Architecture

### 3.1 Transport & Types
- Add `src/lib/http.ts` exporting `createHttpClient({ baseURL })`.
- Responsibilities: attach `Content-Type`, inject telemetry headers, handle timeouts via `AbortController`, auto JSON parsing, convert HTTP failures into `{ status, code, message, diagnostics }`.
- Generate TypeScript contracts from source of truth. Options:
  1. Introduce OpenAPI spec (preferred) and run `npx openapi-typescript docs/frontend-api.yaml -o src/lib/api-types.ts`.
  2. Or, use `oapi-codegen` against Go handlers to emit JSON schema, then convert to TS.
- Guard each response with `zod` schemas to catch drift early (e.g., `const StrategySchema = z.object({ name: z.string(), ... })`).

### 3.2 Domain Modules
Create a folder structure:
```
src/lib/api/
  http.ts           // transport
  schemas.ts        // zod schemas
  strategies.ts
  modules.ts
  providers.ts
  adapters.ts
  instances.ts
  risk.ts
  runtime-config.ts
  context.ts
  outbox.ts
```
Each module exports both raw functions (`fetchStrategies(params)`) and React Query hooks (`useStrategies(params)`).

### 3.3 State Management & Caching
- Install TanStack Query (React Query) if not already available: `pnpm add @tanstack/react-query`.
- Wrap `app/layout.tsx` with `QueryClientProvider`.
- Establish query key taxonomy, e.g., `['strategies']`, `['strategyModules', filters]`, `['providers']`, `['instances', id, 'orders', params]`.
- Encode refetch behaviors:
  - Providers auto-refetch every 2s when any provider is `starting`.
  - Instances history tabs lazy-load on dialog open using `enabled: historyDialogOpen`.
  - Strategy modules list caches results for 30s to avoid repeated backend hits while filtering.

### 3.4 Error Handling & Telemetry
- Extend `ApiError` with `kind` hints (validation, conflict, unavailable, network). Map backend `status` to UI states.
- Centralize toast/alert messaging so repeated patterns (validation error vs network offline) share copy.
- Add optional telemetry hook (e.g., `logApiRequest`), logging duration, status, and endpoint to console/Sentry in dev.

---

## 4. Feature Enablement Roadmap

### 4.1 Strategies & Modules
- **Detail drawer**: clicking a strategy shows metadata (`GET /strategies/{name}`), last refresh timestamp, and quick links to associated modules (via `/strategies/modules?strategy=name`).
- **Usage overlay**: show running instances per module using `/strategies/modules/{selector}/usage`.
- **Registry export**: add “Download registry” action calling `GET /strategies/registry`, offering JSON download + copy to clipboard.
- **Targeted refresh**: surface `/strategies/refresh` payload builder (hash/tag selectors) so operators can selectively reload modules.

### 4.2 Providers & Adapters
- **Shared hook** for providers returning `{ data, startProvider, stopProvider, refetch }` encapsulating all mutation logic.
- **Balances tab**: reuse `/providers/{name}/balances` with filter/search and integrate into provider details + instance history (balances currently reachable only via instances modal).
- **Adapter drilldown**: from provider form, open adapter detail fetched via `GET /adapters/{identifier}` and autofill config placeholders with `settingsSchema`; highlight which fields are secret (mask values using existing `MASKED_SECRET_PLACEHOLDER`).

### 4.3 Instances & History
- Replace manual `Promise.all` loading with `useInstances`, `useStrategies`, `useProviders`, `useStrategyModules`.
- Introduce `useInstance(id)` hook hitting `GET /strategy/instances/{id}` to show live status/diff vs spec.
- In the history dialog, convert `fetchHistory` functions into lazily enabled queries keyed by `(id, tab, params)`.
- Add CSV export for orders/executions by reusing query data.

### 4.4 Risk & Context
- Replace manual normalization with zod schema that enforces numeric ranges and trimmed strings.
- Provide diff summary when saving risk config (before calling `PUT /risk/limits`).
- Extend context backup page:
  - Upload + sanitize payload (already available in `context-backup.ts`).
  - Preview diff vs live snapshot.
  - Execute restore via `POST /context/backup` with confirmation dialog.

### 4.5 Runtime Config & Outbox (New pages)
- **Runtime config**: new route `/config` showing JSON editor (monaco/ace). Use `/config/runtime` GET/PUT/DELETE with diff view, metadata banner (source, persistedAt, filePath).
- **Outbox monitor**: list `/outbox` events with filters for aggregate type/id, pagination, delete action to clear stuck entries. Link from provider detail to filtered view (e.g., aggregateID=provider name).

---

## 5. Implementation Plan (Phased)

### Phase 0 – Preparation (0.5 day)
- Install dependencies (`@tanstack/react-query`, `zod`, `msw` if not already).
- Document environment variables (`NEXT_PUBLIC_API_URL` default).

### Phase 1 – Transport & Typings (1.5 days)
- Generate `api-types.ts`.
- Build `httpClient` with timeout + error normalization.
- Write zod schemas for high-risk payloads (strategies, modules, providers, instances, risk config, runtime config snapshot).

### Phase 2 – Domain Modules (2 days)
- Break `api-client.ts` into domain files using new transport.
- Update existing pages to import from the new modules without yet adding React Query—keep behavior parity.
- Remove unused helper functions once migrations complete.

### Phase 3 – React Query Integration (2 days)
- Introduce `QueryClientProvider` and convert pages incrementally:
  1. Strategies & adapters (read-only, low risk).
  2. Providers (introduce mutations + invalidation).
  3. Instances (largest change; stage carefully).
- Delete legacy `useEffect` fetch logic after verifying parity.

### Phase 4 – Feature Enhancements (3 days)
- Implement strategy detail drawer, module usage overlays, registry export.
- Add provider balances tab, adapter schema modal, and improved polling.
- Ship runtime config editor, context restore workflow, and outbox screen.

### Phase 5 – Testing & Documentation (1 day)
- Add MSW-based tests per hook (e.g., `useProviders.spec.tsx` verifying caching/invalidation).
- Document data-layer usage in `docs/frontend-architecture.md` (add diagrams showing hooks stack).
- Update README/QUICKSTART with new scripts (`pnpm test:unit`, `pnpm msw` etc.).

### Phase 6 – Hardening (0.5 day)
- Run Lighthouse/Playwright smoke tests to ensure new flows behave.
- Gather QA feedback, monitor logs for parsing errors, adjust zod schemas as needed.

---

## 6. Developer Onboarding Checklist
1. **Install deps**: `cd web/client && pnpm install`.
2. **Start backend**: `make run` from repo root (ensures API at `:8880`).
3. **Bootstrap configs**: `cp config/app.example.yaml config/app.yaml`, adjust providers as needed.
4. **Run frontend dev server**: `pnpm dev` (set `NEXT_PUBLIC_API_URL=http://localhost:8880` if backend differs).
5. **Add React Query devtools** (optional) for inspection while building.
6. **Enable MSW**: configure `src/mocks/handlers.ts` for offline testing; run `pnpm test` to ensure new hooks have coverage.

---

## 7. Risks & Mitigations
- **Type generation drift**: automate via `pnpm generate:api-types` script tied to Go `make generate`.
- **Large refactor touching many files**: merge in phases; behind feature flags if needed (e.g., gating new runtime config page).
- **Backend schema changes**: zod parsers will surface errors early; log and degrade gracefully (show fallback message).
- **State thrash during migration**: convert one page at a time, keeping both old/new hooks behind toggles if necessary.

---

## 8. Reference Resources
- Backend API contract: see Section 9 below for the embedded HTTP API contract.
- Existing UI for context backup and strategy modules: `web/client/src/app/context/backup`, `.../strategies/modules`.
- Utility helpers worth reusing: `src/lib/context-backup.ts` (sanitization), `src/lib/utils.ts` (string helpers).

With the above plan, the frontend developer can begin by scaffolding the shared HTTP layer, then iteratively migrate each feature area while unlocking the remaining backend capabilities. Reach out to the platform team if any endpoint behavior differs from the documented contract.
---

## 9. Control Plane HTTP API Reference
This section inlines the previous `docs/plans/frontend/frontend-api.md` contract so the upgrade plan and API surface stay in sync.

### Meltica Control Plane HTTP API

Base URL defaults to `http://localhost:8880`. All responses are UTF-8 JSON with permissive CORS. Request bodies are limited to 1 MiB; oversized payloads receive `413 Request Entity Too Large`. Errors are emitted as `{"status":"error","error":"message"}` with appropriate HTTP codes.

---

#### Table of Contents

1. [Strategies](#strategies)
2. [Strategy Modules](#strategy-modules)
3. [Strategy Registry](#strategy-registry)
4. [Providers & Adapters](#providers--adapters)
5. [Instances & History](#instances--history)
6. [Orders, Executions, Balances](#orders-executions-balances)
7. [Risk Limits](#risk-limits)
8. [Context Backup & Restore](#context-backup--restore)
9. [Outbox](#outbox)
10. [Common Payloads](#common-payloads)
11. [Quality Gates](#quality-gates)

---

#### Strategies

##### `GET /strategies`
Returns every registered strategy.

```json
{
  "strategies": [
    {
      "name": "momentum",
      "displayName": "Momentum",
      "version": "1.3.0",
      "description": "...",
      "config": [{ "name": "lookback", "type": "duration", "...": "..." }],
      "events": ["ExecReport", "Ticker"]
    }
  ]
}
```

##### `GET /strategies/{name}`
Looks up a strategy by name (case-insensitive). `404` when missing.

---

#### Strategy Modules

##### `GET /strategies/modules`
Query params:

| Param          | Type    | Notes                                    |
|----------------|---------|------------------------------------------|
| `strategy`     | string  | Filter by strategy name                  |
| `hash`         | string  | Filter by revision hash                  |
| `runningOnly`  | bool    | `true` limits to modules with live usage |
| `limit`        | int ≥0  | Defaults to no paging                    |
| `offset`       | int ≥0  | Defaults to 0                            |

Response:

```json
{
  "modules": [ { "name": "momentum", "file": "...", "hash": "...", "...": "..." } ],
  "total": 3,
  "offset": 0,
  "limit": 50,
  "strategyDirectory": "/abs/path/to/strategies"
}
```

##### `POST /strategies/modules`
Create or update a JS module.

```json
{
  "filename": "momentum.js",
  "name": "momentum",
  "tag": "stable",
  "aliases": ["mom-v1"],
  "promoteLatest": true,
  "source": "// full JS source"
}
```

Response `201`:

```json
{
  "status": "pending_refresh",
  "strategyDirectory": "/workspace/strategies",
  "module": { "name": "momentum", "hash": "abc123", "tag": "stable", "version": "1.3.0", "file": "momentum.js", "path": "/workspace/strategies/momentum.js" }
}
```

##### `GET /strategies/modules/{name}`
Fetch module summary (`js.ModuleSummary`).

##### `PUT /strategies/modules/{name}`
Same payload as POST; returns updated resolution with `200 OK`.

##### `DELETE /strategies/modules/{name}`
Removes the module (`204 No Content`).

##### `GET /strategies/modules/{name}/source`
Returns `application/javascript` body of the module.

##### `GET /strategies/modules/{name}/usage`
Query params: `includeStopped` (bool), `limit`, `offset`. Response:

```json
{
  "selector": "momentum:stable",
  "strategy": "momentum",
  "hash": "abc123",
  "usage": { "strategy": "momentum", "hash": "abc123", "instances": ["inst-1"], "count": 1, "firstSeen": "...", "lastSeen": "...", "running": true },
  "instances": [
    {
      "instanceSummary": { "id": "inst-1", "providers": ["..."], "...": "..." },
      "links": { "self": "/strategy/instances/inst-1", "usage": "/strategy/instances/inst-1/executions" }
    }
  ],
  "total": 1,
  "offset": 0,
  "limit": 10
}
```

---

#### Strategy Registry

##### `POST /strategies/refresh`
Body optional:
```json
{ "hashes": ["abc123"], "strategies": ["momentum:stable"] }
```
Empty body refreshes everything. Responses:
- Full refresh: `{"status":"refreshed"}`
- Targeted: `{"status":"partial_refresh","results":[{...}]}`.

##### `GET /strategies/registry`
Returns registry manifest plus runtime usage:
```json
{
  "registry": {
    "momentum": {
      "tags": { "stable": "abc123" },
      "hashes": { "abc123": { "tag": "stable", "path": "/workspace/momentum.js" } }
    }
  },
  "usage": [
    { "strategy": "momentum", "hash": "abc123", "instances": ["inst-1"], "count": 1, "firstSeen": "...", "lastSeen": "...", "running": true }
  ]
}
```

---

#### Providers & Adapters

##### `GET /providers`
```json
{
  "providers": [
    {
      "name": "binance-spot",
      "adapter": "binance",
      "identifier": "binance",
      "instrumentCount": 120,
      "settings": { "...": "..." },
      "running": true,
      "status": "running",
      "startupError": "",
      "dependentInstances": ["arb-eur"],
      "dependentInstanceCount": 1
    }
  ]
}
```

##### `POST /providers`
```json
{
  "name": "binance-spot",
  "adapter": {
    "identifier": "binance",
    "config": { "apiKey": "abc", "apiSecret": "xyz" }
  },
  "enabled": true
}
```
Response `202 Accepted` with `provider.RuntimeDetail` and `Location: /providers/{name}`. `enabled=true` triggers async start.

##### `GET /providers/{name}`
Detailed runtime metadata with instrument catalog and adapter schema.

##### `PUT /providers/{name}`
Same payload as POST. Body `name` must match path (case-insensitive). Response `200`.

##### `DELETE /providers/{name}`
Fails with `409` if any instances depend on the provider. Success payload `{"status":"removed","name":"..."}`.

##### Provider Actions
- `POST /providers/{name}/start` → async start (202).
- `POST /providers/{name}/stop` → stop instance (200).
- `GET /providers/{name}/balances` → see [Orders, Executions, Balances](#orders-executions-balances).

##### `GET /adapters`
Lists adapter metadata (identifier, displayName, venue, capability list, `settingsSchema` definitions).

##### `GET /adapters/{identifier}`
Fetch single adapter metadata, `404` if unknown.

---

#### Instances & History

##### `GET /strategy/instances`
```json
{
  "instances": [
    {
      "instanceSummary": {
        "id": "arb-eur",
        "strategyIdentifier": "momentum",
        "strategyTag": "stable",
        "strategyHash": "abc123",
        "strategyVersion": "1.3.0",
        "strategySelector": "momentum:stable",
        "providers": ["binance-spot","okx-spot"],
        "aggregatedSymbols": ["BTCEUR","ETHEUR"],
        "running": true,
        "usage": { "...": "..." }
      },
      "links": { "self": "/strategy/instances/arb-eur", "usage": "/strategies/modules/momentum/usage" }
    }
  ]
}
```

##### `POST /strategy/instances`
```json
{
  "id": "arb-eur",
  "strategy": { "identifier": "momentum", "config": { "lookback": "5m" } },
  "scope": {
    "binance-spot": { "symbols": ["BTCEUR","ETHEUR"] },
    "okx-spot": { "symbols": ["BTCEUR"] }
  }
}
```
Response `201` with snapshot, including inferred selector/tag/hash/version fields and provider list.

##### `GET /strategy/instances/{id}`
Returns snapshot plus hypermedia links.

##### `PUT /strategy/instances/{id}`
Same payload as POST. Body `id` must match path (or be omitted). Response `200`.

##### `DELETE /strategy/instances/{id}`
Returns `{"status":"removed","id":"..."}`.

##### Instance Actions
- `POST /strategy/instances/{id}/start` or `/stop` → `{"status":"ok","id":"...","action":"start|stop"}`.
- `GET /strategy/instances/{id}/orders`
  - Query params: `limit` (default 50), multiple `state=ACK`, `provider`.
  - Response `{"orders": orderstore.OrderRecord[], "count": n}`.
- `GET /strategy/instances/{id}/executions`
  - Query: `limit` (default 100), `provider`, `orderId`.
  - Response `{"executions": orderstore.ExecutionRecord[], "count": n}`.

---

#### Orders, Executions, Balances

Schemas are provided by `internal/domain/orderstore/store.go`.

- **OrderRecord**: base order fields plus `acknowledgedAt`, `completedAt`, `createdAt`, `updatedAt`. Metadata keys use camelCase (e.g., `exchangeOrderId`, `filledQuantity`, `remainingQty`, `avgFillPrice`, `rejectReason`, `commissionAmount`, `commissionAsset`).
- **ExecutionRecord**: includes `fee`, `feeAsset`, `liquidity`, `tradedAt`, metadata (e.g., `remainingQty`, `state`, `eventSymbol`).
- **BalanceRecord**: per-provider asset totals with `snapshotAt`, `createdAt`, `updatedAt`.

##### `GET /providers/{name}/balances`
Query: `limit` (default 100, max 500), `asset`. Response `{"balances": BalanceRecord[], "count": n}`.

---

#### Risk Limits

##### `GET /risk/limits`
Returns current `config.RiskConfig`:

```json
{
  "limits": {
    "maxPositionSize": "10",
    "maxNotionalValue": "500000",
    "notionalCurrency": "USD",
    "orderThrottle": 5,
    "orderBurst": 3,
    "maxConcurrentOrders": 50,
    "priceBandPercent": 1.5,
    "allowedOrderTypes": ["limit","market"],
    "killSwitchEnabled": true,
    "maxRiskBreaches": 3,
    "circuitBreaker": { "threshold": 5, "cooldown": "5m", "enabled": true }
  }
}
```

##### `PUT /risk/limits`
Accepts the same structure; blanks trimmed, numeric defaults applied, allowed order types normalized to lowercase. Response `{"status":"updated","limits":{...}}`.

---

#### Context Backup & Restore

##### `GET /context/backup`
Snapshot:

```json
{
  "providers": [ { "name": "binance-spot", "adapter": "binance", "config": { "...": "..." } } ],
  "lambdas": [ { "id": "arb-eur", "strategy": { "...": "..." }, "scope": { "...": "..." } } ],
  "risk": { "...": "..." }
}
```

##### `POST /context/backup`
Reapplies providers, lambdas, and risk config. Response `{"status":"restored"}`.

---

#### Outbox

##### `GET /outbox`
Query `limit` (default 100, max 500). Response:

```json
{
  "events": [
    {
      "id": 42,
      "aggregateType": "provider",
      "aggregateID": "binance-spot",
      "eventType": "Trade",
      "payload": { "eventId": "evt-1", "instrument": "BTCEUR" },
      "headers": { "provider": "binance-spot", "symbol": "BTCEUR", "eventId": "evt-1" },
      "availableAt": "2024-06-01T12:00:00Z",
      "publishedAt": null,
      "attempts": 0,
      "lastError": "",
      "delivered": false,
      "createdAt": "2024-06-01T12:00:00Z"
    }
  ],
  "count": 1
}
```

##### `DELETE /outbox/{id}`
Deletes an entry. Response `{"id":42,"status":"deleted"}`.

---

#### Common Payloads

- **Strategy Metadata** (`strategies.Metadata`): `name`, `displayName`, `version`, `description`, `config[]`, `events[]`.
- **Module Summary** (`js.ModuleSummary`): file/path/hash/version/tags, alias maps, revision history, running usage, strategy metadata.
- **Provider Metadata** (`provider.RuntimeMetadata`): adapter identifiers, instrumentation counts, runtime status, dependent instances.
- **Provider Detail** (`provider.RuntimeDetail`): `RuntimeMetadata` + `schema.Instrument[]` + `provider.AdapterMetadata`.
- **Lambda Spec** (`config.LambdaSpec`): `id`, strategy block (identifier/config/selector/tag/hash/version), scope map.
- **Risk Config**: see [Risk Limits](#risk-limits).
- **Orderstore Records**: described above.
- **Outbox Record** (`outboxstore.EventRecord`): aggregate identifiers, event payload/headers, timing metadata, attempts, delivery status.

---

#### Quality Gates

Run before shipping API changes:

```bash
make lint   # golangci-lint run --config .golangci.yml
make test   # go test ./... -race -count=1 -timeout=30s
```

Both currently pass (see latest logs).

---

