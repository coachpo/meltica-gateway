'use client';

import { useCallback } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createProvider,
  deleteProvider,
  fetchAdapter,
  fetchAdapters,
  fetchProvider,
  fetchProviderBalances,
  fetchProviders,
  startProvider,
  stopProvider,
  updateProvider,
  type BalanceHistoryFilters,
} from '@/lib/api/providers';
import type {
  AdapterMetadata,
  BalanceHistoryResponse,
  Provider,
  ProviderDetail,
  ProviderRequest,
} from '@/lib/types';
import { queryKeys } from './query-keys';
import { useApiNotifications } from './use-api-notifications';

export function useProvidersQuery(enabled = true) {
  return useQuery<Provider[]>({
    queryKey: queryKeys.providers(),
    queryFn: fetchProviders,
    enabled,
    staleTime: 5_000,
    refetchInterval: (query) => {
      const current = (query.state.data as Provider[] | undefined) ?? [];
      return current.some((provider) => provider.status === 'starting') ? 2_000 : false;
    },
  });
}

export function useProviderQuery(name?: string, enabled = false) {
  const key = name ?? '__pending__';
  return useQuery<ProviderDetail>({
    queryKey: queryKeys.provider(key),
    queryFn: () => {
      if (!name) {
        throw new Error('Provider name missing');
      }
      return fetchProvider(name);
    },
    enabled: Boolean(name && enabled),
    staleTime: 5_000,
  });
}

export function useAdaptersQuery(enabled = true) {
  return useQuery<AdapterMetadata[]>({
    queryKey: queryKeys.adapters(),
    queryFn: fetchAdapters,
    enabled,
    staleTime: 60_000,
  });
}

export function useAdapterQuery(identifier?: string, enabled = false) {
  const key = identifier ?? '__pending__';
  return useQuery<AdapterMetadata>({
    queryKey: ['adapter', key],
    queryFn: () => {
      if (!identifier) {
        throw new Error('Adapter identifier missing');
      }
      return fetchAdapter(identifier);
    },
    enabled: Boolean(identifier && enabled),
  });
}

export function useProviderBalancesQuery(
  name?: string,
  filters?: BalanceHistoryFilters,
  enabled = false,
) {
  const key = name ?? '__pending__';
  return useQuery<BalanceHistoryResponse>({
    queryKey: queryKeys.providerBalances(key, filters),
    queryFn: () => {
      if (!name) {
        throw new Error('Provider name missing');
      }
      return fetchProviderBalances(name, filters);
    },
    enabled: Boolean(name && enabled),
  });
}

export function useProviderLoader() {
  const queryClient = useQueryClient();
  return useCallback(
    async (name: string) => {
      if (!name) {
        throw new Error('Provider name missing');
      }
      return queryClient.fetchQuery({
        queryKey: queryKeys.provider(name),
        queryFn: () => fetchProvider(name),
      });
    },
    [queryClient],
  );
}

function invalidateProviderCaches(queryClient: ReturnType<typeof useQueryClient>, name?: string) {
  void queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
  if (name) {
    void queryClient.invalidateQueries({ queryKey: queryKeys.provider(name) });
  }
}

export function useCreateProviderMutation() {
  const queryClient = useQueryClient();
  const { notifySuccess, notifyError } = useApiNotifications();

  return useMutation<ProviderDetail, unknown, ProviderRequest>({
    mutationFn: (payload) => createProvider(payload),
    onSuccess: (provider) => {
      notifySuccess({
        title: 'Provider created',
        description: `${provider.name} added successfully.`,
      });
      invalidateProviderCaches(queryClient, provider.name);
    },
    onError: (error) => {
      notifyError({
        title: 'Create failed',
        error,
        fallbackMessage: 'Unable to create provider.',
      });
    },
  });
}

export function useUpdateProviderMutation() {
  const queryClient = useQueryClient();
  const { notifySuccess, notifyError } = useApiNotifications();

  return useMutation<ProviderDetail, unknown, { name: string; payload: ProviderRequest }>({
    mutationFn: ({ name, payload }) => updateProvider(name, payload),
    onSuccess: (provider) => {
      notifySuccess({
        title: 'Provider updated',
        description: `${provider.name} updated successfully.`,
      });
      invalidateProviderCaches(queryClient, provider.name);
    },
    onError: (error) => {
      notifyError({
        title: 'Update failed',
        error,
        fallbackMessage: 'Unable to update provider.',
      });
    },
  });
}

export function useDeleteProviderMutation() {
  const queryClient = useQueryClient();
  const { notifySuccess, notifyError } = useApiNotifications();

  return useMutation<void, unknown, string>({
    mutationFn: (name) => deleteProvider(name),
    onSuccess: (_, name) => {
      notifySuccess({
        title: 'Provider deleted',
        description: `${name} removed successfully.`,
      });
      invalidateProviderCaches(queryClient, name);
    },
    onError: (error) => {
      notifyError({
        title: 'Delete failed',
        error,
        fallbackMessage: 'Unable to delete provider.',
      });
    },
  });
}

export function useStartProviderMutation() {
  const queryClient = useQueryClient();
  const { notifySuccess, notifyError } = useApiNotifications();

  return useMutation<ProviderDetail, unknown, string>({
    mutationFn: (name) => startProvider(name),
    onSuccess: (provider) => {
      notifySuccess({
        title: 'Provider started',
        description: `${provider.name} is starting.`,
      });
      invalidateProviderCaches(queryClient, provider.name);
    },
    onError: (error) => {
      notifyError({
        title: 'Start failed',
        error,
        fallbackMessage: 'Unable to start provider.',
      });
    },
  });
}

export function useStopProviderMutation() {
  const queryClient = useQueryClient();
  const { notifySuccess, notifyError } = useApiNotifications();

  return useMutation<ProviderDetail, unknown, string>({
    mutationFn: (name) => stopProvider(name),
    onSuccess: (provider) => {
      notifySuccess({
        title: 'Provider stopped',
        description: `${provider.name} is stopping.`,
      });
      invalidateProviderCaches(queryClient, provider.name);
    },
    onError: (error) => {
      notifyError({
        title: 'Stop failed',
        error,
        fallbackMessage: 'Unable to stop provider.',
      });
    },
  });
}
