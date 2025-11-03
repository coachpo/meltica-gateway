import { expect, test } from '@playwright/test';
import { apiClient } from '../src/lib/api-client';

const originalFetch = globalThis.fetch;

test.afterEach(() => {
  globalThis.fetch = originalFetch;
});

test('getStrategyModuleSource returns raw JavaScript content', async () => {
  const calls: Array<{ input: Parameters<typeof fetch>[0]; init?: RequestInit | undefined }> =
    [];
  globalThis.fetch = (async (input: Parameters<typeof fetch>[0], init?: RequestInit) => {
    calls.push({ input, init });
    return {
      ok: true,
      status: 200,
      text: async () => 'module.exports = {};',
    } as unknown as Response;
  }) as typeof fetch;

  const source = await apiClient.getStrategyModuleSource('alpha.js');

  expect(source).toBe('module.exports = {};');
  expect(String(calls[0].input)).toBe(
    'http://localhost:8880/strategies/modules/alpha.js/source',
  );
  expect(calls[0].init?.method ?? 'GET').toBe('GET');
});

test('createStrategyModule issues POST with JSON payload', async () => {
  let capturedBody: string | undefined;
  globalThis.fetch = (async (_input: Parameters<typeof fetch>[0], init?: RequestInit) => {
    capturedBody = typeof init?.body === 'string' ? (init.body as string) : undefined;
    return {
      ok: true,
      status: 201,
      text: async () =>
        JSON.stringify({
          status: 'pending_refresh',
          strategyDirectory: '/srv/strategies',
          module: {
            name: 'alpha',
            hash: 'sha256:abc123',
            tag: 'v1.0.0',
            version: 'v1.0.0',
            file: 'alpha.js',
          },
        }),
    } as unknown as Response;
  }) as typeof fetch;

  const response = await apiClient.createStrategyModule({
    filename: 'alpha.js',
    source: 'module.exports = {};'
  });

  expect(capturedBody).toBe(
    JSON.stringify({ filename: 'alpha.js', source: 'module.exports = {};' }),
  );
  expect(response.status).toBe('pending_refresh');
  expect(response.strategyDirectory).toBe('/srv/strategies');
  expect(response.module).toBeDefined();
  expect(response.module?.hash).toBe('sha256:abc123');
});

test('refreshStrategies returns status payload', async () => {
  globalThis.fetch = (async () => {
    return {
      ok: true,
      status: 200,
      text: async () => JSON.stringify({ status: 'refreshed' }),
    } as unknown as Response;
  }) as typeof fetch;

  const response = await apiClient.refreshStrategies();

  expect(response.status).toBe('refreshed');
});
