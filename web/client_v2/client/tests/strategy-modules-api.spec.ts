import { expect, test } from '@playwright/test';
import {
  createStrategyModule,
  exportStrategyRegistry,
  fetchStrategyModuleSource,
  fetchStrategyModules,
  fetchStrategyModuleUsage,
  refreshStrategyCatalog,
} from '../src/lib/api/strategies';
import { StrategyValidationError } from '../src/lib/api/errors';

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

  const source = await fetchStrategyModuleSource('alpha.js');

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

  const response = await createStrategyModule({
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

  const response = await refreshStrategyCatalog();

  expect(response.status).toBe('refreshed');
});

test('getStrategyModules applies query parameters', async () => {
  let requestedUrl = '';
  globalThis.fetch = (async (input: Parameters<typeof fetch>[0]) => {
    requestedUrl = String(input);
    return {
      ok: true,
      status: 200,
      text: async () =>
        JSON.stringify({
          modules: [],
          total: 0,
          offset: 20,
          limit: 10,
          strategyDirectory: '/srv/strategies',
        }),
    } as unknown as Response;
  }) as typeof fetch;

  const response = await fetchStrategyModules({
    strategy: 'grid',
    hash: 'sha256:abc',
    runningOnly: true,
    limit: 10,
    offset: 20,
  });

  expect(requestedUrl).toBe(
    'http://localhost:8880/strategies/modules?strategy=grid&hash=sha256%3Aabc&runningOnly=true&limit=10&offset=20',
  );
  expect(response.strategyDirectory).toBe('/srv/strategies');
});

test('refreshStrategies sends payload when provided', async () => {
  let capturedBody: string | undefined;
  globalThis.fetch = (async (_input: Parameters<typeof fetch>[0], init?: RequestInit) => {
    capturedBody = typeof init?.body === 'string' ? (init.body as string) : undefined;
    return {
      ok: true,
      status: 200,
      text: async () =>
        JSON.stringify({
          status: 'partial_refresh',
          results: [
            { selector: 'grid:canary', reason: 'refreshed', hash: 'sha256:new', instances: ['grid-eu-1'] },
          ],
        }),
    } as unknown as Response;
  }) as typeof fetch;

  const response = await refreshStrategyCatalog({
    strategies: ['grid:canary'],
    hashes: ['sha256:new'],
  });

  expect(capturedBody).toBe(
    JSON.stringify({ strategies: ['grid:canary'], hashes: ['sha256:new'] }),
  );
  expect(response.status).toBe('partial_refresh');
  expect(response.results?.[0]?.reason).toBe('refreshed');
});

test('getStrategyModuleUsage fetches usage endpoint', async () => {
  let requestedUrl = '';
  globalThis.fetch = (async (input: Parameters<typeof fetch>[0]) => {
    requestedUrl = String(input);
    return {
      ok: true,
      status: 200,
      text: async () =>
        JSON.stringify({
          selector: 'grid@sha256:abc',
          strategy: 'grid',
          hash: 'sha256:abc',
          usage: { strategy: 'grid', hash: 'sha256:abc', count: 1, instances: ['grid-eu-1'] },
          instances: [],
          total: 1,
          offset: 0,
          limit: 25,
        }),
    } as unknown as Response;
  }) as typeof fetch;

  const result = await fetchStrategyModuleUsage('grid@sha256:abc', {
    limit: 25,
    offset: 0,
    includeStopped: true,
  });

  expect(requestedUrl).toBe(
    'http://localhost:8880/strategies/modules/grid%40sha256%3Aabc/usage?limit=25&offset=0&includeStopped=true',
  );
  expect(result.selector).toBe('grid@sha256:abc');
  expect(result.usage.count).toBe(1);
});

test('createStrategyModule surfaces structured validation diagnostics', async () => {
  globalThis.fetch = (async () => {
    return {
      ok: false,
      status: 422,
      text: async () =>
        JSON.stringify({
          error: 'strategy_validation_failed',
          message: 'Metadata validation failed',
          diagnostics: [
            { stage: 'compile', message: 'Unexpected token', line: 7, column: 2 },
            { stage: 'validation', message: 'displayName required' },
          ],
        }),
    } as unknown as Response;
  }) as typeof fetch;

  await expect(
    createStrategyModule({
      filename: 'alpha.js',
      source: 'module.exports = {}',
    }),
  ).rejects.toThrowError(StrategyValidationError);

  try {
    await createStrategyModule({
      filename: 'alpha.js',
      source: 'module.exports = {}',
    });
  } catch (err) {
    expect(err instanceof StrategyValidationError).toBe(true);
    if (err instanceof StrategyValidationError) {
      expect(err.message).toBe('Metadata validation failed');
      expect(err.diagnostics).toHaveLength(2);
      expect(err.diagnostics?.[0]?.stage).toBe('compile');
      expect(err.diagnostics?.[0]?.line).toBe(7);
    }
  }
});

test('exportStrategyRegistry requests registry endpoint', async () => {
  let requestedUrl = '';
  globalThis.fetch = (async (input: Parameters<typeof fetch>[0]) => {
    requestedUrl = String(input);
    return {
      ok: true,
      status: 200,
      text: async () => JSON.stringify({ registry: {}, usage: [] }),
    } as unknown as Response;
  }) as typeof fetch;

  const result = await exportStrategyRegistry();

  expect(requestedUrl).toBe('http://localhost:8880/strategies/registry');
  expect(result).toEqual({ registry: {}, usage: [] });
});
