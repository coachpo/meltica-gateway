import { ReactNode } from 'react';
import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useStrategiesQuery, useStrategyQuery } from '@/lib/hooks/use-strategies';

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

describe('useStrategiesQuery', () => {
  it('loads strategy list', async () => {
    const wrapper = createWrapper();
    const { result } = renderHook(() => useStrategiesQuery(), { wrapper });

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });

    expect(result.current.data?.length).toBe(1);
    expect(result.current.data?.[0].name).toBe('momentum');
  });
});

describe('useStrategyQuery', () => {
  it('fetches a specific strategy when enabled', async () => {
    const wrapper = createWrapper();
    const { result } = renderHook(() => useStrategyQuery('momentum', true), { wrapper });

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });

    expect(result.current.data?.displayName).toBe('Momentum');
  });
});
