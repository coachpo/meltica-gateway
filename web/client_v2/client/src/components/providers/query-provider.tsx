'use client';

import type { ReactNode } from 'react';
import { useState } from 'react';
import { QueryClientProvider } from '@tanstack/react-query';
import { ReactQueryDevtools } from '@tanstack/react-query-devtools';
import { createQueryClient } from '@/lib/react-query';

interface QueryProviderProps {
  children: ReactNode;
}

export function QueryProvider({ children }: QueryProviderProps) {
  const [client] = useState(() => createQueryClient());
  const enableDevtools = process.env.NODE_ENV !== 'production';

  return (
    <QueryClientProvider client={client}>
      {children}
      {enableDevtools ? (
        <ReactQueryDevtools initialIsOpen={false} buttonPosition="bottom-right" />
      ) : null}
    </QueryClientProvider>
  );
}
