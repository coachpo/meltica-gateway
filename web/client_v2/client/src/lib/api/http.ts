import { z } from 'zod';
import type {
  ApiErrorPayload,
  StrategyDiagnostic,
  StrategyErrorResponse,
  StrategyValidationErrorResponse,
} from '@/lib/types';
import { ApiError, StrategyValidationError } from './errors';

const DEFAULT_TIMEOUT_MS = 8000;
const DEFAULT_BASE_URL = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8880';

type HttpMethod = 'GET' | 'POST' | 'PUT' | 'DELETE';

type SearchParamValue =
  | string
  | number
  | boolean
  | null
  | undefined
  | Array<string | number | boolean | null | undefined>;

export interface BaseRequestOptions {
  path: string;
  method?: HttpMethod;
  searchParams?: Record<string, SearchParamValue>;
  signal?: AbortSignal;
  headers?: HeadersInit;
  timeoutMs?: number | null;
}

export interface JsonRequestOptions<TSchema extends z.ZodTypeAny | undefined = z.ZodTypeAny>
  extends BaseRequestOptions {
  body?: unknown;
  schema?: TSchema;
}

export interface TextRequestOptions extends BaseRequestOptions {
  body?: unknown;
}

export interface HttpClient {
  request(options: BaseRequestOptions & { body?: unknown }): Promise<Response>;
  requestJson<TSchema extends z.ZodTypeAny>(
    options: JsonRequestOptions<TSchema> & { schema: TSchema },
  ): Promise<z.infer<TSchema>>;
  requestJson(options: JsonRequestOptions<undefined> & { schema?: undefined }): Promise<void>;
  requestJson<TSchema extends z.ZodTypeAny | undefined>(
    options: JsonRequestOptions<TSchema>,
  ): Promise<TSchema extends z.ZodTypeAny ? z.infer<TSchema> : void>;
  requestText(options: TextRequestOptions): Promise<string>;
}

type TelemetryHeadersProvider =
  | (() => HeadersInit | null | undefined | Promise<HeadersInit | null | undefined>)
  | null
  | undefined;

export interface HttpClientConfig {
  baseURL?: string;
  timeoutMs?: number;
  defaultHeaders?: HeadersInit;
  telemetryHeaders?: TelemetryHeadersProvider;
  fetchImplementation?: typeof fetch;
}

const defaultTelemetryHeaders: NonNullable<TelemetryHeadersProvider> = () => ({
  'x-meltica-client': 'web',
  'x-meltica-request-id': generateRequestId(),
  'x-meltica-sent-at': new Date().toISOString(),
});

export function createHttpClient(config: HttpClientConfig = {}): HttpClient {
  const baseURL = config.baseURL ?? DEFAULT_BASE_URL;
  const timeoutMs = config.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const fetchImpl = config.fetchImplementation ?? fetch;
  const defaultHeaders = new Headers(config.defaultHeaders);
  const telemetryProvider = config.telemetryHeaders ?? defaultTelemetryHeaders;

  async function sendRequest(options: BaseRequestOptions & { body?: unknown }): Promise<Response> {
    const { path, method = 'GET', body, searchParams, signal, headers, timeoutMs: perRequestTimeout } = options;
    const url = buildURL(baseURL, path, searchParams);
    const requestHeaders = new Headers(defaultHeaders);
    const telemetry = await telemetryProvider?.();
    mergeHeaders(requestHeaders, telemetry);
    mergeHeaders(requestHeaders, headers);
    prepareHeaders(requestHeaders, body);

    const { signal: resolvedSignal, cleanup } = createRequestSignal(perRequestTimeout ?? timeoutMs, signal);

    try {
      const response = await fetchImpl(url, {
        method,
        signal: resolvedSignal,
        headers: requestHeaders,
        body: serializeBody(method, body),
      });
      if (!response.ok) {
        await handleError(response);
      }
      return response;
    } finally {
      cleanup();
    }
  }

  function requestJson<TSchema extends z.ZodTypeAny>(
    options: JsonRequestOptions<TSchema> & { schema: TSchema },
  ): Promise<z.infer<TSchema>>;
  function requestJson(
    options: JsonRequestOptions<undefined> & { schema?: undefined },
  ): Promise<void>;
  async function requestJson<TSchema extends z.ZodTypeAny | undefined>(
    options: JsonRequestOptions<TSchema>,
  ): Promise<TSchema extends z.ZodTypeAny ? z.infer<TSchema> : void> {
    const response = await sendRequest(options);
    const data = await parseJSONResponse(response);
    if (!options.schema) {
      return undefined as TSchema extends z.ZodTypeAny ? z.infer<TSchema> : void;
    }
    return options.schema.parse(data) as TSchema extends z.ZodTypeAny ? z.infer<TSchema> : void;
  }

  async function requestText(options: TextRequestOptions): Promise<string> {
    const response = await sendRequest(options);
    return response.text();
  }

  return {
    request: sendRequest,
    requestJson,
    requestText,
  };
}

function buildURL(baseURL: string, path: string, params?: Record<string, SearchParamValue>): URL {
  const trimmedPath = path.startsWith('/') ? path : `/${path}`;
  const url = new URL(trimmedPath, baseURL);
  if (!params) {
    return url;
  }
  Object.entries(params).forEach(([key, value]) => {
    if (value === undefined || value === null) {
      return;
    }
    if (Array.isArray(value)) {
      value.forEach((entry) => {
        if (entry === undefined || entry === null) {
          return;
        }
        url.searchParams.append(key, formatSearchValue(entry));
      });
      return;
    }
    url.searchParams.set(key, formatSearchValue(value));
  });
  return url;
}

function formatSearchValue(value: string | number | boolean): string {
  if (typeof value === 'boolean') {
    return value ? 'true' : 'false';
  }
  return String(value);
}

function mergeHeaders(target: Headers, value?: HeadersInit | null): void {
  if (!value) {
    return;
  }
  new Headers(value).forEach((headerValue, headerKey) => {
    target.set(headerKey, headerValue);
  });
}

function prepareHeaders(headers: Headers, body: unknown): void {
  if (body instanceof FormData) {
    return;
  }
  if (!headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json');
  }
}

function serializeBody(method: HttpMethod, body: unknown): BodyInit | undefined {
  if (body === undefined || method === 'GET' || method === 'HEAD') {
    return undefined;
  }
  if (body instanceof FormData || typeof body === 'string' || body instanceof Blob) {
    return body as BodyInit;
  }
  return JSON.stringify(body);
}

function createRequestSignal(timeoutMs?: number | null, upstream?: AbortSignal) {
  if ((!timeoutMs || timeoutMs <= 0) && !upstream) {
    return { signal: undefined, cleanup: () => {} } as const;
  }
  if (!timeoutMs || timeoutMs <= 0) {
    return {
      signal: upstream,
      cleanup: () => {},
    } as const;
  }
  const controller = new AbortController();
  const timeoutId = setTimeout(() => {
    controller.abort(new Error(`Request timed out after ${timeoutMs}ms`));
  }, timeoutMs);

  const forwardAbort = () => {
    controller.abort(upstream?.reason ?? new Error('Request aborted'));
  };
  if (upstream) {
    if (upstream.aborted) {
      controller.abort(upstream.reason);
    } else {
      upstream.addEventListener('abort', forwardAbort, { once: true });
    }
  }

  const cleanup = () => {
    clearTimeout(timeoutId);
    if (upstream) {
      upstream.removeEventListener('abort', forwardAbort);
    }
  };

  return { signal: controller.signal, cleanup } as const;
}

function safeParseJSON(text: string): unknown {
  if (!text) {
    return undefined;
  }
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return undefined;
  }
}

function parseDiagnostics(source: unknown): StrategyDiagnostic[] {
  if (!Array.isArray(source)) {
    return [];
  }
  return source
    .map((entry) => {
      if (!entry || typeof entry !== 'object') {
        return null;
      }
      const record = entry as Record<string, unknown>;
      const stage =
        typeof record.stage === 'string' && record.stage.trim().length > 0
          ? record.stage.trim()
          : undefined;
      const message =
        typeof record.message === 'string' && record.message.trim().length > 0
          ? record.message.trim()
          : undefined;
      if (!stage && !message) {
        return null;
      }
      const line =
        typeof record.line === 'number'
          ? record.line
          : Number.isFinite(record.line)
            ? Number(record.line)
            : undefined;
      const column =
        typeof record.column === 'number'
          ? record.column
          : Number.isFinite(record.column)
            ? Number(record.column)
            : undefined;
      const hint =
        typeof record.hint === 'string' && record.hint.trim().length > 0
          ? record.hint.trim()
          : undefined;
      return {
        ...(stage ? { stage } : {}),
        ...(message ? { message } : {}),
        ...(line !== undefined ? { line } : {}),
        ...(column !== undefined ? { column } : {}),
        ...(hint ? { hint } : {}),
      };
    })
    .filter((entry): entry is StrategyDiagnostic => Boolean(entry));
}

function parseErrorPayload(payload: unknown): StrategyErrorResponse | ApiErrorPayload | null {
  if (!payload || typeof payload !== 'object') {
    return null;
  }
  const record = payload as Record<string, unknown>;
  const diagnostics = parseDiagnostics(record.diagnostics);
  const message =
    typeof record.message === 'string' && record.message.trim().length > 0
      ? record.message.trim()
      : undefined;
  const error =
    typeof record.error === 'string' && record.error.trim().length > 0
      ? record.error.trim()
      : undefined;
  const status =
    typeof record.status === 'string' && record.status.trim().length > 0
      ? record.status.trim()
      : undefined;

  if (!status && !error && !message && diagnostics.length === 0) {
    return null;
  }

  return {
    status,
    error: error ?? 'request_failed',
    message,
    diagnostics,
  };
}

async function handleError(response: Response): Promise<never> {
  const bodyText = await response.text();
  const payload = safeParseJSON(bodyText);
  const parsed = parseErrorPayload(payload);
  if (response.status === 422 && parsed?.error === 'strategy_validation_failed') {
    const message =
      parsed.message && parsed.message.trim().length > 0
        ? parsed.message
        : 'Strategy validation failed';
    throw new StrategyValidationError(message, {
      response: parsed as StrategyValidationErrorResponse,
      diagnostics: parsed.diagnostics ?? [],
    });
  }
  const message =
    parsed?.message && parsed.message.trim().length > 0
      ? parsed.message
      : parsed?.error && parsed.error.trim().length > 0
        ? parsed.error
        : `Request failed with status ${response.status}`;
  throw new ApiError(message, { status: response.status, payload: parsed });
}

async function parseJSONResponse(response: Response): Promise<unknown> {
  const text = await response.text();
  if (!text) {
    return undefined;
  }
  try {
    return JSON.parse(text) as unknown;
  } catch {
    throw new ApiError('Invalid JSON response', { status: response.status });
  }
}

const defaultClient = createHttpClient();

export const requestJson = defaultClient.requestJson;
export const requestText = defaultClient.requestText;
export const sendRequest = defaultClient.request;
export const httpClient = defaultClient;

function generateRequestId(): string {
  const globalCrypto = typeof globalThis !== 'undefined' ? globalThis.crypto : undefined;
  if (globalCrypto && typeof globalCrypto.randomUUID === 'function') {
    return globalCrypto.randomUUID();
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}
