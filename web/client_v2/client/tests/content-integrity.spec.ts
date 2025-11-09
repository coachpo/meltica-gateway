import { expect, test } from '@playwright/test';

const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8880';

const LONG_DESCRIPTION =
  'Maintains a rolling order book mesh with adaptive liquidity bands to absorb sudden market dislocations without leaking spread.';

const LONG_CONFIG_DESCRIPTION =
  'Controls the adaptive decay for quote velocities across fast-changing markets with persistent tail detection.';

test.describe('Galaxy content integrity', () => {
  test.beforeEach(async ({ page }) => {
    await page.route(`${API_BASE}/strategies`, (route) => {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          strategies: [
            {
              name: 'hyperdrive-liquidity',
              displayName: 'Hyperdrive Liquidity Engine',
              description: LONG_DESCRIPTION,
              version: '3.4.2',
              config: [
                {
                  name: 'quote_decay',
                  type: 'number',
                  description: LONG_CONFIG_DESCRIPTION,
                  default: 0.875,
                  required: true,
                },
              ],
              events: ['Ticker', 'OrderBook', 'BalanceUpdate'],
            },
          ],
        }),
      });
    });
  });

  test('renders long descriptions without truncation', async ({ page }) => {
    await page.goto('/strategies');

    const strategyCard = page.getByRole('button', { name: /Hyperdrive Liquidity Engine/ });
    await expect(strategyCard).toBeVisible();

    const description = strategyCard.locator('[data-slot="card-description"]');
    await expect(description).toHaveText(LONG_DESCRIPTION);

    const measurements = await description.evaluate((element) => ({
      scrollWidth: element.scrollWidth,
      clientWidth: element.clientWidth,
    }));

    expect(measurements.scrollWidth - measurements.clientWidth).toBeLessThanOrEqual(2);
  });
});
