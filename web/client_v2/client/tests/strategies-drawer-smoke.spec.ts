import { expect, test } from '@playwright/test';

const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8880';
const STRATEGY_NAME = 'momentum';

test.describe('Strategy drawer smoke', () => {
  test.beforeEach(async ({ page }) => {
    await page.route(`${API_BASE}/strategies`, (route) => {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          strategies: [
            {
              name: STRATEGY_NAME,
              displayName: 'Momentum',
              description: 'Demo momentum strategy',
              version: '1.2.0',
              config: [
                {
                  name: 'lookback',
                  type: 'duration',
                  description: 'Window size',
                  default: '5m',
                  required: true,
                },
              ],
              events: ['Ticker'],
            },
          ],
        }),
      });
    });

    await page.route(`${API_BASE}/strategies/${STRATEGY_NAME}`, (route) => {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          name: STRATEGY_NAME,
          displayName: 'Momentum',
          description: 'Demo momentum strategy',
          version: '1.2.0',
          config: [
            {
              name: 'lookback',
              type: 'duration',
              description: 'Window size',
              default: '5m',
              required: true,
            },
          ],
          events: ['Ticker'],
        }),
      });
    });

    await page.route(new RegExp(`${API_BASE}/strategies/modules.*`), (route) => {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          modules: [
            {
              name: STRATEGY_NAME,
              file: 'momentum.js',
              path: '/workspace/momentum.js',
              hash: 'abc123',
              version: '1.2.0',
              tags: ['stable'],
              tagAliases: { latest: 'abc123' },
              revisions: [],
              running: [
                {
                  hash: 'abc123',
                  instances: ['momentum-eu'],
                  count: 1,
                  firstSeen: '2024-01-01T00:00:00Z',
                  lastSeen: '2024-01-01T00:05:00Z',
                },
              ],
              size: 2048,
              metadata: {
                name: STRATEGY_NAME,
                displayName: 'Momentum',
                description: 'Demo momentum strategy',
                version: '1.2.0',
                config: [],
                events: ['Ticker'],
              },
            },
          ],
          total: 1,
          offset: 0,
          limit: 50,
        }),
      });
    });
  });

  test('opens the drawer and renders metadata + module usage summary', async ({ page }) => {
    await page.goto('/strategies');
    await expect(page.getByRole('heading', { name: 'Strategies' })).toBeVisible();

    const cardButton = page.getByRole('button', { name: /Momentum/ });
    await cardButton.click();

    const drawer = page.getByRole('dialog', { name: /Strategy details/ });
    await expect(drawer.getByText('Inspect schema, events, and module usage')).toBeVisible();

    await expect(drawer.getByText('lookback')).toBeVisible();
    await expect(drawer.getByText('duration')).toBeVisible();
    await expect(drawer.getByText('Modules & usage')).toBeVisible();
    await expect(drawer.getByText('abc123', { exact: false })).toBeVisible();
    await expect(drawer.getByText('1 module')).toBeVisible();
  });
});
