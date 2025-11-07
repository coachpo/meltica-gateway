# Frontend Architecture

## 1. Overview
The Meltica control UI is a Next.js client application that treats the Go control-plane API as its single source of truth. All network access flows through:

1. A transport layer (`src/lib/api/http.ts`) that unifies telemetry headers, request timeouts, and error normalization.
2. Domain-specific modules inside `src/lib/api/` that expose buffered functions per area (strategies, providers, instances, risk, etc.).
3. React Query hooks in `src/lib/hooks/` that wrap each domain method for cache-aware reads and mutations.

This structure ensures every page uses the same caching, retry, and telemetry semantics.

## 2. Transport Layer
`createHttpClient` (in `src/lib/api/http.ts`) is the only place `fetch` is invoked. Notable behaviors:

- Injects Meltica telemetry headers (`x-meltica-client`, `x-meltica-request-id`, `x-meltica-sent-at`).
- Adds `AbortController` timeouts (8s by default) and propagates upstream cancellation.
- Normalizes API errors into `ApiError`/`StrategyValidationError` so UI surfaces consistent toasts.
- Exports `requestJson`, `requestText`, and `sendRequest` helpers consumed by domain modules.

When adding new calls, prefer `requestJson({ path, method, body, schema })` so you automatically gain validation and error handling.

## 3. Type Generation & Validation
The API surface is defined in `docs/frontend-api.yaml`. Running `pnpm generate:api-types` emits the latest contracts to `src/lib/api-types.ts`, and `src/lib/types.ts` re-exports those definitions for the rest of the app.

Validation happens in two layers:

- Domain modules pass zod schemas (`src/lib/api/schemas.ts`) to `requestJson` to catch drift.
- React components receive fully typed data via the generated TypeScript interfaces.

Always regenerate types when backend structs change and commit the resulting `api-types.ts` diff.

## 4. Domain Modules & Hooks
Each folder in `src/lib/api/` mirrors a backend area. Example: `providers.ts` handles CRUD, start/stop, adapter metadata, and balances. Modules own:

- Building request payloads & query params.
- Selecting the zod schema for responses.
- Normalizing responses (e.g., runtime config snapshots).

Hooks in `src/lib/hooks/` (e.g., `useProvidersQuery`, `useProviderQuery`, `useStartProviderMutation`) wrap those functions with query keys declared in `src/lib/hooks/query-keys.ts`. Hooks should:

- Use specific `queryKey`s so invalidation is surgical.
- Configure `staleTime`, `refetchInterval`, or `enabled` flags based on UX requirements.
- Forward `useApiNotifications` for consistent toast messaging during mutations.

## 5. Server-State Surfaces
Pages never call `fetch` directly; they import hooks instead:

- **Instances** (`/instances`): `useInstancesQuery`, `useInstanceOrdersQuery`, etc., manage polling/caching for list & history tabs.
- **Strategies** (`/strategies`): `useStrategiesQuery` for the grid plus `useStrategyQuery`/`useStrategyModulesQuery` inside the new drawer.
- **Providers** (`/providers`): Drawer tabs combine `useProviderQuery` and `useProviderBalancesQuery` so runtime metadata & telemetry share a cache.
- **Adapters** (`/adapters`): The schema modal reuses the global adapters query; no extra fetch is required.
- **Strategy Modules** (`/strategies/modules`): The usage overlay, registry export, and CRUD flows all rely on shared hooks to keep caching consistent.

Whenever you need additional data in a page, add or extend a hook rather than performing ad-hoc `fetch` calls inside components.

## 6. Adding a New Endpoint
1. **Update the schema**: Add the route/structures to `docs/frontend-api.yaml` and run `pnpm generate:api-types`.
2. **Define schemas**: Extend `src/lib/api/schemas.ts` (or a new schema file) with the zod validator for responses/payloads.
3. **Implement domain functions**: Create helper(s) in the relevant `src/lib/api/*` file that call `requestJson`.
4. **Expose hooks**: Wrap the domain method in `src/lib/hooks/` with an appropriate `queryKey` and options.
5. **Use in pages**: Import the hook inside components and rely on React Query state instead of custom state machines.

## 7. Tooling & Scripts
- `pnpm dev` – start Next.js locally (set `NEXT_PUBLIC_API_URL` if the control plane differs from `http://localhost:8880`).
- `pnpm generate:api-types` – regenerate contracts after editing `docs/frontend-api.yaml`.
- `pnpm lint` – project lint (ESLint).
- `pnpm test` / `pnpm test:unit` – Vitest + MSW hook tests.
- `pnpm test:unit:watch` – watch mode for the Vitest suite.
- `pnpm test:e2e` – Playwright smoke tests; requires a running frontend server (`PLAYWRIGHT_BASE_URL`) because tests route traffic through the browser.

## 8. Testing Strategy

1. **MSW-backed hook tests**
   - Mock control-plane endpoints via `src/mocks/handlers.ts` and `src/mocks/server.ts`.
   - `vitest.setup.ts` polyfills `fetch`/`Headers` (`cross-fetch`), boots MSW, and tears it down between specs.
   - Hooks are tested via `@testing-library/react` renderHook + isolated `QueryClient` instances under `src/lib/hooks/__tests__`.

2. **Playwright smoke coverage**
   - `playwright.config.ts` scopes tests in `tests/`.
   - Specs such as `tests/strategies-drawer-smoke.spec.ts` route API calls inline, open real UI flows, and assert critical elements render.
   - Intended for CI smoke coverage; base URL is configurable via `PLAYWRIGHT_BASE_URL`.

## 9. Future Enhancements
Planned hardening work includes:

- Surfacing telemetry/logging when zod parsing fails in the transport.
- Wiring Lighthouse runs into CI alongside Playwright.
- Expanding Playwright coverage to runtime config/context restore flows.

Document new behaviors here as they land so the architecture guide remains the onboarding entry point for frontend contributors.
