'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { deleteOutboxEvent, fetchOutboxEvents } from '@/lib/api/outbox';
import type { OutboxDeleteResponse, OutboxListResponse, OutboxQuery } from '@/lib/types';
import { queryKeys } from './query-keys';
import { useApiNotifications } from './use-api-notifications';

export function useOutboxQuery(filters?: OutboxQuery, enabled = true) {
  return useQuery<OutboxListResponse>({
    queryKey: queryKeys.outbox(filters),
    queryFn: () => fetchOutboxEvents(filters),
    enabled,
    staleTime: 5_000,
    refetchInterval: 10_000,
  });
}

export function useDeleteOutboxEventMutation(filters?: OutboxQuery) {
  const queryClient = useQueryClient();
  const { notifySuccess, notifyError } = useApiNotifications();

  return useMutation<OutboxDeleteResponse, unknown, number>({
    mutationFn: (id) => deleteOutboxEvent(id),
    onSuccess: () => {
      notifySuccess({
        title: 'Outbox entry deleted',
        description: 'Event removed successfully.',
      });
      void queryClient.invalidateQueries({ queryKey: ['outbox'] });
      if (filters) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.outbox(filters) });
      }
    },
    onError: (error) => {
      notifyError({
        title: 'Delete failed',
        error,
        fallbackMessage: 'Unable to delete outbox entry.',
      });
    },
  });
}
