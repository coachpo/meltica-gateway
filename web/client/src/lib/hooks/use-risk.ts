'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { fetchRiskLimits, updateRiskLimits, type RiskLimitsResult } from '@/lib/api/risk';
import type { RiskConfig } from '@/lib/types';
import { queryKeys } from './query-keys';
import { useApiNotifications } from './use-api-notifications';

export function useRiskLimitsQuery(enabled = true) {
  return useQuery<RiskLimitsResult>({
    queryKey: queryKeys.riskLimits(),
    queryFn: fetchRiskLimits,
    enabled,
    staleTime: 15_000,
  });
}

export function useUpdateRiskLimitsMutation() {
  const queryClient = useQueryClient();
  const { notifySuccess, notifyError } = useApiNotifications();

  return useMutation<RiskLimitsResult, unknown, RiskConfig>({
    mutationFn: (config) => updateRiskLimits(config),
    onSuccess: () => {
      notifySuccess({
        title: 'Risk limits saved',
        description: 'Risk configuration updated successfully.',
      });
      void queryClient.invalidateQueries({ queryKey: queryKeys.riskLimits() });
    },
    onError: (error) => {
      notifyError({
        title: 'Save failed',
        error,
        fallbackMessage: 'Unable to update risk limits.',
      });
    },
  });
}
