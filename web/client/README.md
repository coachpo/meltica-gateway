# Meltica Control Client

Next.js web client for managing Meltica trading gateway strategies and configurations.

## Features

- **Dashboard**: Overview of all control plane features
- **Strategy Instances**: Create, start, stop, and delete strategy instances with full lifecycle management
- **Strategies**: Browse available trading strategies and their configuration schemas
- **Providers**: View exchange provider connections and instrument catalogs
- **Adapters**: Explore exchange adapter definitions and capabilities
- **Risk Limits**: Configure position limits, order throttling, and circuit breakers

## Tech Stack

- **Framework**: Next.js 16 with App Router
- **Language**: TypeScript
- **Styling**: Tailwind CSS v4
- **Components**: shadcn/ui
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

- `pnpm dev` - Start development server with Turbopack
- `pnpm build` - Build production bundle
- `pnpm start` - Start production server
- `pnpm lint` - Run ESLint

## Project Structure

```
src/
├── app/                    # Next.js App Router pages
│   ├── adapters/          # Adapter listing page
│   ├── instances/         # Strategy instance management
│   ├── providers/         # Provider monitoring page
│   ├── risk/              # Risk limits configuration
│   ├── strategies/        # Strategy catalog page
│   ├── layout.tsx         # Root layout with navigation
│   └── page.tsx           # Dashboard landing page
├── components/
│   ├── nav.tsx            # Main navigation component
│   └── ui/                # shadcn/ui components
├── lib/
│   ├── api-client.ts      # REST API client
│   ├── types.ts           # TypeScript type definitions
│   └── utils.ts           # Utility functions
└── ...
```

## API Client

The API client (`src/lib/api-client.ts`) provides typed methods for all Control API endpoints:

### Strategy Catalog
- `getStrategies()` - List all strategies
- `getStrategy(name)` - Get strategy details

### Providers
- `getProviders()` - List all providers
- `getProvider(name)` - Get provider details with instruments

### Adapters
- `getAdapters()` - List all adapters
- `getAdapter(identifier)` - Get adapter metadata

### Strategy Instances
- `getInstances()` - List all instances
- `getInstance(id)` - Get instance details
- `createInstance(spec)` - Create and start instance
- `updateInstance(id, spec)` - Update instance configuration
- `deleteInstance(id)` - Delete instance
- `startInstance(id)` - Start stopped instance
- `stopInstance(id)` - Stop running instance

### Risk Limits
- `getRiskLimits()` - Get current risk configuration
- `updateRiskLimits(config)` - Update risk limits

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
