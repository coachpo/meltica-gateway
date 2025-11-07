'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  fetchConfigBackup,
  fetchRuntimeConfig,
  restoreConfigBackup,
  revertRuntimeConfig,
  updateRuntimeConfig,
} from '@/lib/api/config';
import type {
  ConfigBackup,
  RestoreConfigResponse,
  RuntimeConfig,
  RuntimeConfigSnapshot,
} from '@/lib/types';
import { queryKeys } from './query-keys';
import { useApiNotifications } from './use-api-notifications';

export function useRuntimeConfigQuery(enabled = true) {
  return useQuery<RuntimeConfigSnapshot>({
    queryKey: queryKeys.runtimeConfig(),
    queryFn: fetchRuntimeConfig,
    enabled,
    staleTime: 15_000,
  });
}

export function useUpdateRuntimeConfigMutation() {
  const queryClient = useQueryClient();
  const { notifySuccess, notifyError } = useApiNotifications();

  return useMutation<RuntimeConfigSnapshot, unknown, RuntimeConfig>({
    mutationFn: (payload) => updateRuntimeConfig(payload),
    onSuccess: () => {
      notifySuccess({
        title: 'Runtime updated',
        description: 'Runtime configuration saved successfully.',
      });
      void queryClient.invalidateQueries({ queryKey: queryKeys.runtimeConfig() });
    },
    onError: (error) => {
      notifyError({
        title: 'Update failed',
        error,
        fallbackMessage: 'Unable to update runtime configuration.',
      });
    },
  });
}

export function useRevertRuntimeConfigMutation() {
  const queryClient = useQueryClient();
  const { notifySuccess, notifyError } = useApiNotifications();

  return useMutation<RuntimeConfigSnapshot>({
    mutationFn: () => revertRuntimeConfig(),
    onSuccess: () => {
      notifySuccess({
        title: 'Runtime reverted',
        description: 'Runtime configuration reverted to persisted snapshot.',
      });
      void queryClient.invalidateQueries({ queryKey: queryKeys.runtimeConfig() });
    },
    onError: (error) => {
      notifyError({
        title: 'Revert failed',
        error,
        fallbackMessage: 'Unable to revert runtime configuration.',
      });
    },
  });
}

export function useConfigBackupQuery(enabled = false) {
  return useQuery<ConfigBackup>({
    queryKey: queryKeys.configBackup(),
    queryFn: fetchConfigBackup,
    enabled,
    staleTime: 60_000,
  });
}

export function useRestoreConfigBackupMutation() {
  const queryClient = useQueryClient();
  const { notifySuccess, notifyError } = useApiNotifications();

  return useMutation<RestoreConfigResponse, unknown, ConfigBackup>({
    mutationFn: (payload) => restoreConfigBackup(payload),
    onSuccess: () => {
      notifySuccess({
        title: 'Config restored',
        description: 'Configuration backup applied successfully.',
      });
      void queryClient.invalidateQueries({ queryKey: queryKeys.configBackup() });
      void queryClient.invalidateQueries({ queryKey: queryKeys.runtimeConfig() });
    },
    onError: (error) => {
      notifyError({
        title: 'Restore failed',
        error,
        fallbackMessage: 'Unable to restore configuration backup.',
      });
    },
  });
}
