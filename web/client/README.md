# Meltica Control Client

Next.js web client for managing Meltica trading gateway strategies and configurations.

## Features

- **Dashboard**: Overview of all control plane features
- **Strategy Instances**: Create, start, stop, and delete strategy instances with full lifecycle management
- **Strategies**: Browse available trading strategies and their configuration schemas
- **Providers**: View exchange provider connections and instrument catalogs
- **Adapters**: Explore exchange adapter definitions and capabilities
- **Risk Limits**: Configure position limits, order throttling, and circuit breakers
- **Runtime Config**: Inspect and edit the gateway runtime snapshot
- **Config Backup**: Download and restore persisted configuration bundles
- **Outbox**: Monitor control-plane event deliveries and clean up failures

## Tech Stack

- **Framework**: Next.js 16 with App Router
- **Language**: TypeScript
- **Styling**: Tailwind CSS v4
- **Components**: shadcn/ui
- **Data Layer**: TanStack React Query v5
- **Package Manager**: pnpm
- **Icons**: lucide-react

## Prerequisites

- Node.js 22.20.0 or higher
- pnpm 10.20.0 or higher
- Meltica gateway running on `http://localhost:8880`

## Getting Started

1. **Install dependencies**:
   ```bash
   pnpm install
   ```

2. **Configure API endpoint** (optional):
   
   Create or edit `.env.local`:
   ```
   NEXT_PUBLIC_API_URL=http://localhost:8880
   ```

3. **Start the development server**:
   ```bash
   pnpm dev
   ```

4. **Open the application**:
   
   Navigate to [http://localhost:3000](http://localhost:3000)

## Available Scripts

- `pnpm dev` – Start development server with Turbopack
- `pnpm build` – Build production bundle
- `pnpm start` – Start production server
- `pnpm lint` – Run ESLint
- `pnpm test` – Run unit/integration tests (Vitest + MSW)
- `pnpm test:unit:watch` – Watch mode for the Vitest suite
- `pnpm test:e2e` – Run Playwright smoke tests (requires running frontend + API stubs)

## Project Structure

```
src/
├── app/                    # Next.js App Router pages
│   ├── adapters/          # Adapter listing page
│   ├── instances/         # Strategy instance management
│   ├── providers/         # Provider monitoring page
│   ├── risk/              # Risk limits configuration
│   ├── config/            # Runtime/config backup management
│   ├── outbox/            # Outbox inspection
│   ├── strategies/        # Strategy catalog page
│   ├── layout.tsx         # Root layout with navigation
│   └── page.tsx           # Dashboard landing page
├── components/
│   ├── nav.tsx            # Main navigation component
│   └── ui/                # shadcn/ui components
├── lib/
│   ├── api/               # Domain-oriented REST modules
│   ├── hooks/             # React Query hooks & helpers
│   ├── types.ts           # TypeScript type definitions
│   └── utils.ts           # Utility functions
└── ...
```

## Data Layer

All API access flows through domain-specific modules in `src/lib/api/`. Each module exports request helpers plus Zod schemas to validate responses before they reach React Query caches. Corresponding hooks live under `src/lib/hooks/` and wrap common queries or mutations with cache keys, toast notifications, and error handling.

## Component Library

This project uses [shadcn/ui](https://ui.shadcn.com/) components. To add new components:

```bash
pnpm dlx shadcn@latest add <component-name>
```

## Development Notes

- All pages use React Server Components by default; interactive pages are marked with `'use client'`
- API calls are made client-side for real-time updates
- The backend CORS middleware allows requests from any origin during development
- Error handling displays user-friendly messages via alerts and dialogs

## Testing

1. **Unit & hook tests (Vitest + MSW)**
   ```bash
   pnpm test        # one-off run
   pnpm test:unit   # alias
   pnpm test:unit:watch
   ```
   - Uses MSW handlers defined in `src/mocks/handlers.ts` to stub the control-plane API.
   - Fetch is polyfilled via `vitest.setup.ts` so hooks run in a Node environment.

2. **Playwright smoke tests**
   ```bash
   pnpm dev &
   PLAYWRIGHT_BASE_URL=http://localhost:3000 pnpm test:e2e
   ```
   - The `tests/strategies-drawer-smoke.spec.ts` spec exercises the new strategy drawer.
   - API calls are intercepted inline via `page.route`. Adjust `PLAYWRIGHT_BASE_URL` if the dev server runs on another address.

## Production Deployment

1. Build the application:
   ```bash
   pnpm build
   ```

2. Set production environment variables:
   ```
   NEXT_PUBLIC_API_URL=https://your-gateway-url
   ```

3. Start the production server:
   ```bash
   pnpm start
   ```

For containerized deployment, refer to the Next.js [Docker documentation](https://nextjs.org/docs/app/building-your-application/deploying#docker-image).
