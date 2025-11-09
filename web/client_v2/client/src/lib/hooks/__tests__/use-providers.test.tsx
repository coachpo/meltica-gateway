import { ReactNode } from 'react';
import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useProvidersQuery, useProviderQuery } from '@/lib/hooks/use-providers';

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });

  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

describe('useProvidersQuery', () => {
  it('returns providers from the API', async () => {
    const wrapper = createWrapper();
    const { result } = renderHook(() => useProvidersQuery(), { wrapper });

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });

    expect(result.current.data?.length).toBe(1);
    expect(result.current.data?.[0].name).toBe('binance-spot');
  });
});

describe('useProviderQuery', () => {
  it('fetches a provider detail when enabled', async () => {
    const wrapper = createWrapper();
    const { result } = renderHook(() => useProviderQuery('binance-spot', true), {
      wrapper,
    });

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });

    expect(result.current.data?.identifier).toBe('binance');
  });
});
