'use client';

import { useCallback } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createInstance,
  deleteInstance,
  fetchInstance,
  fetchInstanceExecutions,
  fetchInstanceOrders,
  fetchInstances,
  startInstance,
  stopInstance,
  updateInstance,
  type ExecutionHistoryFilters,
  type OrderHistoryFilters,
} from '@/lib/api/instances';
import { fetchProviderBalances } from '@/lib/api/providers';
import type { InstanceActionResponse } from '@/lib/api/schemas';
import type {
  BalanceRecord,
  ExecutionHistoryResponse,
  InstanceSpec,
  InstanceSummary,
  OrderHistoryResponse,
} from '@/lib/types';
import { queryKeys } from './query-keys';
import { useApiNotifications } from './use-api-notifications';

export function useInstancesQuery(enabled = true) {
  return useQuery<InstanceSummary[]>({
    queryKey: queryKeys.instances(),
    queryFn: fetchInstances,
    enabled,
    staleTime: 5_000,
    refetchInterval: 5_000,
  });
}

export function useInstanceQuery(id?: string, enabled = false) {
  const key = id ?? '__pending__';
  return useQuery<InstanceSpec>({
    queryKey: queryKeys.instance(key),
    queryFn: () => {
      if (!id) {
        throw new Error('Instance identifier missing');
      }
      return fetchInstance(id);
    },
    enabled: Boolean(id && enabled),
  });
}

export function useInstanceOrdersQuery(
  id?: string,
  filters?: OrderHistoryFilters,
  enabled = false,
) {
  const key = id ?? '__pending__';
  return useQuery<OrderHistoryResponse>({
    queryKey: queryKeys.instanceOrders(key, filters),
    queryFn: () => {
      if (!id) {
        throw new Error('Instance identifier missing');
      }
      return fetchInstanceOrders(id, filters);
    },
    enabled: Boolean(id && enabled),
  });
}

export function useInstanceExecutionsQuery(
  id?: string,
  filters?: ExecutionHistoryFilters,
  enabled = false,
) {
  const key = id ?? '__pending__';
  return useQuery<ExecutionHistoryResponse>({
    queryKey: queryKeys.instanceExecutions(key, filters),
    queryFn: () => {
      if (!id) {
        throw new Error('Instance identifier missing');
      }
      return fetchInstanceExecutions(id, filters);
    },
    enabled: Boolean(id && enabled),
  });
}

function invalidateInstanceCaches(queryClient: ReturnType<typeof useQueryClient>, id?: string) {
  void queryClient.invalidateQueries({ queryKey: queryKeys.instances() });
  void queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
  if (id) {
    void queryClient.invalidateQueries({ queryKey: queryKeys.instance(id) });
    void queryClient.invalidateQueries({ queryKey: ['strategy-modules'] });
  }
}

export function useCreateInstanceMutation() {
  const queryClient = useQueryClient();
  const { notifySuccess, notifyError } = useApiNotifications();

  return useMutation<InstanceSpec, unknown, InstanceSpec>({
    mutationFn: (spec) => createInstance(spec),
    onSuccess: (instance) => {
      notifySuccess({
        title: 'Instance created',
        description: `${instance.id} created successfully.`,
      });
      invalidateInstanceCaches(queryClient, instance.id);
    },
    onError: (error) => {
      notifyError({
        title: 'Create failed',
        error,
        fallbackMessage: 'Unable to create strategy instance.',
      });
    },
  });
}

export function useUpdateInstanceMutation(id: string) {
  const queryClient = useQueryClient();
  const { notifySuccess, notifyError } = useApiNotifications();

  return useMutation<InstanceSpec, unknown, InstanceSpec>({
    mutationFn: (spec) => updateInstance(id, spec),
    onSuccess: (instance) => {
      notifySuccess({
        title: 'Instance updated',
        description: `${instance.id} updated successfully.`,
      });
      invalidateInstanceCaches(queryClient, instance.id);
    },
    onError: (error) => {
      notifyError({
        title: 'Update failed',
        error,
        fallbackMessage: 'Unable to update strategy instance.',
      });
    },
  });
}

export function useDeleteInstanceMutation() {
  const queryClient = useQueryClient();
  const { notifySuccess, notifyError } = useApiNotifications();

  return useMutation<void, unknown, string>({
    mutationFn: (identifier) => deleteInstance(identifier),
    onSuccess: (_, identifier) => {
      notifySuccess({
        title: 'Instance deleted',
        description: `${identifier} removed successfully.`,
      });
      invalidateInstanceCaches(queryClient, identifier);
    },
    onError: (error) => {
      notifyError({
        title: 'Delete failed',
        error,
        fallbackMessage: 'Unable to delete strategy instance.',
      });
    },
  });
}

export function useStartInstanceMutation() {
  const queryClient = useQueryClient();
  const { notifySuccess, notifyError } = useApiNotifications();

  return useMutation<InstanceActionResponse, unknown, string>({
    mutationFn: (identifier) => startInstance(identifier),
    onSuccess: (response) => {
      notifySuccess({
        title: 'Instance starting',
        description: `${response.id} is starting.`,
      });
      invalidateInstanceCaches(queryClient, response.id);
    },
    onError: (error) => {
      notifyError({
        title: 'Start failed',
        error,
        fallbackMessage: 'Unable to start strategy instance.',
      });
    },
  });
}

export function useStopInstanceMutation() {
  const queryClient = useQueryClient();
  const { notifySuccess, notifyError } = useApiNotifications();

  return useMutation<InstanceActionResponse, unknown, string>({
    mutationFn: (identifier) => stopInstance(identifier),
    onSuccess: (response) => {
      notifySuccess({
        title: 'Instance stopping',
        description: `${response.id} is stopping.`,
      });
      invalidateInstanceCaches(queryClient, response.id);
    },
    onError: (error) => {
      notifyError({
        title: 'Stop failed',
        error,
        fallbackMessage: 'Unable to stop strategy instance.',
      });
    },
  });
}

export function useInstanceLoader() {
  const queryClient = useQueryClient();
  return useCallback(
    async (id: string) => {
      if (!id) {
        throw new Error('Instance identifier missing');
      }
      return queryClient.fetchQuery({
        queryKey: queryKeys.instance(id),
        queryFn: () => fetchInstance(id),
      });
    },
    [queryClient],
  );
}

type InstanceBalancesResult = {
  balances: BalanceRecord[];
  count: number;
};

export function useInstanceBalancesQuery(
  providers?: string[],
  limit = 50,
  enabled = false,
) {
  const keyPart =
    providers && providers.length > 0
      ? providers
          .slice()
          .sort((a, b) => a.localeCompare(b))
          .join(',')
      : 'none';
  return useQuery<InstanceBalancesResult>({
    queryKey: ['instance-balances', keyPart, limit],
    enabled: Boolean(enabled && providers && providers.length > 0),
    queryFn: async () => {
      if (!providers || providers.length === 0) {
        return { balances: [], count: 0 };
      }
      const responses = await Promise.all(
        providers.map((provider) => fetchProviderBalances(provider, { limit })),
      );
      const balances = responses.flatMap((res) => res.balances ?? []);
      const count = responses.reduce((sum, res) => sum + (res.count ?? 0), 0);
      return { balances, count };
    },
  });
}
