# Frontend API Upgrade Plan

## 1. Objectives
- Eliminate redundant network traffic and manual `useEffect` data loading by adopting a cache-aware data layer.
- Cover the full Meltica control-plane API, including Outbox, runtime configuration, and backup flows currently absent from the UI.
- Improve developer ergonomics with typed domain clients, shared hooks, and consistent error/reporting patterns.
- Prepare the Next.js app for server-component data fetching, streaming, and suspense-friendly UX.

## 2. Current State Assessment

### 2.1 Data-Fetching Approach
- The entire frontend (`web/client`) issues requests through a singleton class `apiClient` (`src/lib/api-client.ts`, ~800 lines). Each page calls methods such as `apiClient.getProviders()` inside `useEffect`, stores the results in local state, and manages bespoke loading/error flags.
- `apiClient` handles JSON parsing, risk-config normalization, and throws `StrategyValidationError` on 422 responses, but otherwise acts as a thin `fetch` wrapper.
- No caching, deduplication, or AbortController usage exists. Multiple components refetch the same lists independently after every mutation (e.g., `fetchData()` in `src/app/instances/page.tsx` and `refreshProviders()` in `src/app/providers/page.tsx`).

### 2.2 Feature → API Coverage

| Frontend surface | Files | Backend endpoints exercised |
| --- | --- | --- |
| Dashboard cards | `src/app/page.tsx` | none (static) |
| Strategies | `src/app/strategies/page.tsx` | `GET /strategies` |
| Strategy Modules | `src/app/strategies/modules/page.tsx` | `GET/POST/PUT/DELETE /strategies/modules`, `GET /strategies/modules/{id}/source`, `GET /strategies/modules/{selector}/usage`, `POST /strategies/refresh`, `GET /strategies/registry` |
| Providers console | `src/app/providers/page.tsx` | `GET/POST/PUT/DELETE /providers`, `POST /providers/{name}/{start|stop}`, `GET /providers/{name}`, `GET /adapters`, `GET /strategy/instances` |
| Instances workspace | `src/app/instances/page.tsx` | `GET/POST/PUT/DELETE /strategy/instances`, `POST /strategy/instances/{id}/{start|stop}`, `GET /strategy/instances/{id}`, orders/executions/balances history, plus shared lists (`/strategies`, `/providers`, `/strategies/modules`) |
| Risk limits | `src/app/risk/page.tsx` | `GET /risk/limits`, `PUT /risk/limits` |
| Context backup | `src/app/context/backup/page.tsx` | `GET /context/backup`, `POST /context/backup` |
| Adapters | `src/app/adapters/page.tsx` | `GET /adapters` |

### 2.3 Observed Limitations
- **Coverage gaps:** Outbox endpoints and runtime/configuration APIs are missing entirely; detail views (`GET /strategies/{name}`, `/adapters/{id}`) exist server-side but are unused.
- **Performance:** Every refresh refetches all major lists; background polling uses `setInterval` without cancellation; no memoization for provider instrument catalogs.
- **State management:** Dozens of local `useState`/`useEffect` combinations per page yield duplicated loading/error logic and no shared retry/backoff or pagination primitives.
- **Type drift:** `src/lib/types.ts` is manually maintained and does not include Outbox payloads or the latest backend contract fields, risking runtime mismatches.
- **Error reporting:** Mixed UI patterns (inline alerts, toasts, silent failures) and missing telemetry hooks hinder operator insight.

## 3. Backend Coverage Gaps & Opportunities
1. **Outbox visibility:** Add UI + client helpers for `GET /outbox` (limit, filters) and `DELETE /outbox/{id}` to let operators triage stuck events.
2. **Runtime configuration:** Surface `/config/runtime` GET/PUT/DELETE and `/config/backup` GET/POST for full control-plane management, reusing existing serialization helpers.
3. **Strategy/adapter drill-downs:** Reuse `apiClient.getStrategy`, `.getStrategyModule`, `.getAdapter` to populate detail drawers/modals and prefetch data when deep-linking.
4. **Telemetry hooks:** Map API failures to toast + log pipelines, aligning with `docs/TELEMETRY_POINTS.md`.

## 4. Target Architecture

### 4.1 Domain-Oriented API Modules
- Replace the mutable `ApiClient` class with tree-shakeable, pure functions grouped by domain (`src/lib/api/strategies.ts`, `providers.ts`, `instances.ts`, etc.).
- Each module exports:
  - Request builders that accept typed params and return typed promises.
  - `zod` (or TypeScript validator) schemas to parse/validate responses before they reach React Query caches.
  - Shared error normalizers (wrap backend error payloads, `StrategyValidationError`, and network issues into a unified `ApiError` object).

### 4.2 Data Layer with TanStack Query
- Introduce `@tanstack/react-query` with a shared `QueryClient` configured in `src/app/layout.tsx` (client boundary) and `HydrationBoundary` support for server-prefetch.
- Define canonical query keys per entity:
  - `['strategies']`, `['strategy-modules', filters]`, `['providers']`, `['instances']`, `['instance', id]`, `['outbox', params]`, etc.
- Expose hooks under `src/lib/hooks/` that encapsulate queries/mutations, handle retries, and centralize toast/telemetry side effects.

### 4.3 Server Components & Suspense
- For read-heavy pages (Strategies, Adapters, Risk overview), prefetch data in server components using `dehydrate`/`HydrationBoundary` to render meaningful HTML on first paint.
- Use `<Suspense>` boundaries with skeleton components to remove `Loading...` placeholders.

### 4.4 Error Handling & Observability
- Centralize toast + log reporting in a `useApiNotifications` helper that standardizes success/failure messaging.
- Emit structured telemetry (per `TELEMETRY_POINTS.md`) on repeated failures (e.g., provider start). Hook into browser console or remote logger for dev/prod parity.

### 4.5 Type Safety & Tooling
- Generate or validate DTOs:
  - Short term: enrich `src/lib/types.ts` with Outbox + runtime config snapshots plus discriminated unions for mutations.
  - Mid term: adopt `openapi-typescript` (once server publishes OpenAPI) to keep contracts in sync.
- Add ESLint rules to forbid direct `fetch` usage outside `api/` modules to maintain consistency.

### 4.6 Performance Enhancements
- Deduplicate provider instrument lookups with cached queries keyed by provider name.
- Use optimistic updates for toggling provider/instance states to improve UX while background revalidation confirms final status.
- Introduce pagination/virtualization for large tables (modules, outbox events) built atop TanStack Query’s infinite queries when needed.

## 5. Implementation Plan

| Phase | Scope | Key Tasks | Output |
| --- | --- | --- | --- |
| **0. Prep (0.5d)** | Tooling baseline | Add React Query dependency, `QueryClientProvider`, devtools toggle, shared toast/error helper. | App compiles with data layer scaffolding. |
| **1. API Layer Refactor (1d)** | Domain modules | Split `apiClient` into `api/strategies.ts`, `api/providers.ts`, etc.; add Outbox/runtime helpers; cover unit tests for serialization. | Pure function clients + typed DTOs. |
| **2. Simple pages migration (1–1.5d)** | Read-only views | Convert Strategies, Adapters, Risk (read), Context backup snapshot to server-prefetched queries; replace manual state with hooks. | SSR data, suspenseful loaders, consistent errors. |
| **3. Mutations & heavy pages (2–3d)** | Providers, Instances, Strategy Modules | Introduce query/mutation hooks, optimistic updates, targeted invalidations, cached provider detail lookups, derived selectors for builder forms. | Reduced network chatter, better UX. |
| **4. New surfaces (2d)** | Outbox + Runtime Config | Build Outbox list with filters & delete; add Runtime Config editor + Config Backup manager; wire to new API helpers. | Complete backend coverage. |
| **5. Hardening (1d)** | QA | Add unit tests for hooks, Playwright flows for providers/instances/outbox, docs updates (`web/client/README.md`, `docs/frontend-api.md` references). | Release-ready feature branch. |

Durations assume one engineer; parallelization is possible by splitting Phase 3 across providers vs. strategy modules.

## 6. Migration & Rollout Strategy
1. **Dual-path period:** Keep the old `apiClient` available while new hooks roll out; each migrated page imports from `lib/hooks` to minimize blast radius.
2. **Component-by-component migration:** Start with least coupled pages (Strategies, Adapters) to validate infrastructure before tackling Instances/Providers.
3. **Kill switch:** Export a temporary feature flag (env or build-time) to fall back to legacy fetching if critical regressions appear.
4. **Removal:** Once every page uses hooks, delete the legacy `ApiClient` class and enforce lint rule against its reintroduction.

## 7. Testing & Quality Gates
- **Unit tests:** Cover serialization helpers (risk limits normalization, runtime snapshot parsing, Outbox DTOs) and hook logic (query keys, optimistic updates).
- **Integration tests:** Extend Playwright specs under `web/client/tests` to exercise new flows (Outbox delete, provider start/stop, runtime config save/revert).
- **Manual QA:** Exercise `make run` against a seeded backend, verifying caching behavior (React Query Devtools) and ensuring no duplicate requests.
- **Coverage:** Ensure `pnpm test` (lint + unit tests) and `make coverage` (Go backend) remain ≥70 % per repo guidelines.

## 8. Risks & Mitigations
- **Contract drift:** Mitigate via shared schemas and backend-to-frontend type generation. Add CI that validates `docs/frontend-api.md` changes against the client.
- **Cache staleness:** Define per-query stale times (e.g., 5 s for provider status, 60 s for strategies). Use websocket or SSE in future for push updates.
- **Large diff risk:** Keep phases small; merge behind feature flags; document each migration in PR templates.
- **Developer learning curve:** Provide short internal guide on React Query usage, query keys, and hook structure.

## 9. Deliverables
1. React Query infrastructure (providers, devtools, hydration utility).
2. Domain API modules + updated `types.ts` (including Outbox/runtime DTOs).
3. Hook library with consistent error/notification handling.
4. Migrated pages (Strategies, Adapters, Risk, Context backup, Providers, Instances, Strategy Modules).
5. New Outbox + Runtime Config/Backup management UIs.
6. Updated documentation (`web/client/README.md`, `docs/frontend-api.md` references) and automated tests validating the new data layer.
