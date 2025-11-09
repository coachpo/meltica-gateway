import type {
  ApiErrorPayload,
  StrategyDiagnostic,
  StrategyErrorResponse,
  StrategyValidationErrorResponse,
} from '@/lib/types';

interface ApiErrorOptions {
  status?: number;
  payload?: ApiErrorPayload | StrategyErrorResponse | StrategyValidationErrorResponse | null;
  cause?: unknown;
}

export class ApiError extends Error {
  readonly status?: number;
  readonly payload: ApiErrorPayload | StrategyErrorResponse | StrategyValidationErrorResponse | null;

  constructor(message: string, options: ApiErrorOptions = {}) {
    super(message, { cause: options.cause });
    this.name = 'ApiError';
    this.status = options.status;
    this.payload = options.payload ?? null;
  }
}

export class StrategyValidationError extends ApiError {
  readonly response: StrategyValidationErrorResponse | StrategyErrorResponse | null;
  readonly diagnostics: StrategyDiagnostic[];

  constructor(
    message: string,
    options: {
      response?: StrategyValidationErrorResponse | StrategyErrorResponse | null;
      diagnostics?: StrategyDiagnostic[];
    } = {},
  ) {
    super(message, {
      status: 422,
      payload: options.response ?? null,
    });
    this.name = 'StrategyValidationError';
    this.response = options.response ?? null;
    this.diagnostics = options.diagnostics ?? this.response?.diagnostics ?? [];
  }
}

export type StrategyErrorPayload =
  | StrategyValidationErrorResponse
  | StrategyErrorResponse
  | ApiErrorPayload
  | null;

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError;
}

export function toApiError(error: unknown): ApiError {
  if (error instanceof ApiError) {
    return error;
  }
  if (error instanceof Error) {
    return new ApiError(error.message, { cause: error });
  }
  return new ApiError('Unknown API error', { cause: error });
}
