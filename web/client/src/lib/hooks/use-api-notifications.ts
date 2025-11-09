'use client';

import { useCallback } from 'react';
import { useToast } from '@/components/ui/toast-provider';
import type { ApiErrorPayload } from '@/lib/types';

interface SuccessOptions {
  title?: string;
  description?: string;
  duration?: number;
}

interface ErrorOptions extends SuccessOptions {
  error?: unknown;
  fallbackMessage?: string;
  telemetryTag?: string;
  suppressConsole?: boolean;
}

const DEFAULT_ERROR_TITLE = 'Request failed';
const DEFAULT_SUCCESS_TITLE = 'Request completed';

function extractErrorMessage(error: unknown): string | undefined {
  if (!error) {
    return undefined;
  }
  if (typeof error === 'string') {
    return error;
  }
  if (error instanceof Error) {
    return error.message;
  }
  if (typeof error === 'object') {
    const candidate = error as Partial<ApiErrorPayload> & { message?: string; error?: string };
    if (typeof candidate.message === 'string' && candidate.message.trim()) {
      return candidate.message.trim();
    }
    if (typeof candidate.error === 'string' && candidate.error.trim()) {
      return candidate.error.trim();
    }
  }
  return undefined;
}

export function useApiNotifications() {
  const { show } = useToast();

  const notifySuccess = useCallback(
    (options: SuccessOptions = {}) => {
      show({
        variant: 'success',
        title: options.title ?? DEFAULT_SUCCESS_TITLE,
        description: options.description,
        duration: options.duration ?? 4000,
      });
    },
    [show],
  );

  const notifyError = useCallback(
    (options: ErrorOptions = {}) => {
      const description =
        options.description ??
        extractErrorMessage(options.error) ??
        options.fallbackMessage ??
        'Something went wrong. Please try again.';

      if (options.error && !options.suppressConsole) {
        console.error(
          `[api-error${options.telemetryTag ? `:${options.telemetryTag}` : ''}]`,
          options.error,
        );
      }

      show({
        variant: 'destructive',
        title: options.title ?? DEFAULT_ERROR_TITLE,
        description,
        duration: options.duration ?? 5000,
      });
    },
    [show],
  );

  return { notifySuccess, notifyError };
}
