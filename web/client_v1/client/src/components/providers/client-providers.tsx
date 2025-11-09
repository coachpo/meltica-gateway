'use client';

import type { ReactNode } from 'react';
import type { HydrationBoundaryProps } from '@tanstack/react-query';
import { QueryProvider } from './query-provider';
import { QueryHydration } from './query-hydration';
import { ThemeProvider } from '@/components/ui/theme-provider';
import { ToastProvider } from '@/components/ui/toast-provider';

interface ClientProvidersProps {
  children: ReactNode;
  state?: HydrationBoundaryProps['state'];
}

export function ClientProviders({ children, state }: ClientProvidersProps) {
  return (
    <QueryProvider>
      <QueryHydration state={state}>
        <ThemeProvider>
          <ToastProvider>{children}</ToastProvider>
        </ThemeProvider>
      </QueryHydration>
    </QueryProvider>
  );
}
