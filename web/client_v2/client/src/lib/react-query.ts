import { dehydrate, type DehydrateOptions, type HydrationState, QueryClient, type QueryClientConfig } from '@tanstack/react-query';

const defaultConfig: QueryClientConfig = {
  defaultOptions: {
    queries: {
      staleTime: 5_000,
      gcTime: 5 * 60_000,
      refetchOnWindowFocus: false,
      retry: (failureCount, error) => {
        if (error instanceof Error && error.message?.includes('abort')) {
          return false;
        }
        return failureCount < 2;
      },
    },
    mutations: {
      retry: 0,
    },
  },
};

export function createQueryClient(config?: QueryClientConfig) {
  return new QueryClient({
    ...defaultConfig,
    ...config,
    defaultOptions: {
      ...defaultConfig.defaultOptions,
      ...config?.defaultOptions,
      queries: {
        ...defaultConfig.defaultOptions?.queries,
        ...config?.defaultOptions?.queries,
      },
      mutations: {
        ...defaultConfig.defaultOptions?.mutations,
        ...config?.defaultOptions?.mutations,
      },
    },
  });
}

let browserQueryClient: QueryClient | undefined;

export function getBrowserQueryClient(): QueryClient {
  if (!browserQueryClient) {
    browserQueryClient = createQueryClient();
  }
  return browserQueryClient;
}

export function getQueryClient(): QueryClient {
  if (typeof window === 'undefined') {
    return createQueryClient();
  }
  return getBrowserQueryClient();
}

export function dehydrateClient(
  client: QueryClient,
  options?: DehydrateOptions,
): HydrationState {
  return dehydrate(client, options);
}
