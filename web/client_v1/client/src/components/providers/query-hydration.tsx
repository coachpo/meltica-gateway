'use client';

import type { ReactNode } from 'react';
import { HydrationBoundary, type HydrationBoundaryProps } from '@tanstack/react-query';

interface QueryHydrationProps {
  children: ReactNode;
  state?: HydrationBoundaryProps['state'];
}

export function QueryHydration({ children, state }: QueryHydrationProps) {
  return <HydrationBoundary state={state}>{children}</HydrationBoundary>;
}
