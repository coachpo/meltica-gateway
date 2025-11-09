'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { fetchContextBackup, restoreContextBackup } from '@/lib/api/context';
import type { ContextBackupPayload, RestoreContextResponse } from '@/lib/types';
import { queryKeys } from './query-keys';
import { useApiNotifications } from './use-api-notifications';

export function useContextBackupQuery(enabled = true) {
  return useQuery<ContextBackupPayload>({
    queryKey: queryKeys.contextBackup(),
    queryFn: fetchContextBackup,
    enabled,
    staleTime: 60_000,
  });
}

export function useRestoreContextBackupMutation() {
  const queryClient = useQueryClient();
  const { notifySuccess, notifyError } = useApiNotifications();

  return useMutation<RestoreContextResponse, unknown, ContextBackupPayload>({
    mutationFn: (payload) => restoreContextBackup(payload),
    onSuccess: () => {
      notifySuccess({
        title: 'Context restored',
        description: 'Context backup applied successfully.',
      });
      void queryClient.invalidateQueries({ queryKey: queryKeys.contextBackup() });
      void queryClient.invalidateQueries({ queryKey: queryKeys.riskLimits() });
      void queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
      void queryClient.invalidateQueries({ queryKey: queryKeys.instances() });
    },
    onError: (error) => {
      notifyError({
        title: 'Restore failed',
        error,
        fallbackMessage: 'Unable to restore context backup.',
      });
    },
  });
}
