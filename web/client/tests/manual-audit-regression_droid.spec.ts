import { expect, test } from '@playwright/test';

const dashboardCards = [
  { label: /^Strategy Instances/, heading: 'Strategy Instances', path: '/instances' },
  { label: /^Strategies /, heading: 'Strategies', path: '/strategies' },
  { label: /^Strategy Modules/, heading: 'Strategy Modules', path: '/strategies/modules' },
  { label: /^Providers/, heading: 'Providers', path: '/providers' },
  { label: /^Adapters/, heading: 'Adapters', path: '/adapters' },
  { label: /^Risk Limits/, heading: 'Risk Limits', path: '/risk' },
];

test.describe('manual audit regression (droid)', () => {
  test('dashboard call-to-action cards navigate correctly', async ({ page }) => {
    for (const { label, heading, path } of dashboardCards) {
      await page.goto('/');
      await page.getByRole('main').getByRole('link', { name: label }).click();
      await expect(page).toHaveURL(new RegExp(`${path.replace(/[-/\\^$*+?.()|[\]{}]/g, '\\$&')}$`));
      await expect(page.getByRole('heading', { name: heading, level: 1 })).toBeVisible();
    }
  });

  test('instance dialogs open and surface details', async ({ page }) => {
    await page.goto('/instances');

    await page.getByRole('button', { name: 'History' }).first().click();
    const historyDialog = page.getByRole('dialog', { name: /^Instance history/ });
    await expect(historyDialog.getByText('No orders records yet.')).toBeVisible();
    await historyDialog.getByRole('button', { name: 'Close' }).click();

    await page.getByRole('button', { name: 'Edit' }).first().click();
    const editDialog = page.getByRole('dialog', { name: 'Edit Strategy Instance' });
    await expect(editDialog.getByText('grid-demo-1')).toBeVisible();
    await editDialog.getByRole('button', { name: 'Close' }).click();

    await expect(page.getByRole('heading', { name: 'Strategy Instances', level: 1 })).toBeVisible();
  });

  test('strategy modules filter exposes empty state and resets', async ({ page }) => {
    await page.goto('/strategies/modules');
    await expect(page.getByRole('table')).toContainText('delay');

    await page.getByRole('textbox', { name: 'Strategy name' }).fill('zzz');
    await page.getByRole('button', { name: 'Apply filters' }).click();
    await expect(page.getByText('No JavaScript strategies detected')).toBeVisible();

    await page.getByRole('button', { name: 'Reset' }).click();
    await expect(page.getByRole('table')).toContainText('delay');
  });

  test('provider detail modal lists instruments', async ({ page }) => {
    await page.goto('/providers');
    await page.getByRole('button', { name: 'Details' }).first().click();

    const detailDialog = page.getByRole('dialog', { name: 'Provider details' });
    await expect(detailDialog.getByRole('heading', { name: 'Provider details' })).toBeVisible();
    await expect(detailDialog.getByRole('textbox', { name: 'Search symbols…' })).toBeVisible();
    await expect(detailDialog.getByRole('button', { name: 'SOL-USDC SOL / USDC' })).toBeVisible();
    await expect(detailDialog.getByText('Base')).toBeVisible();
    await expect(detailDialog.getByText('SOL')).toBeVisible();

    await detailDialog.getByRole('button', { name: 'Close' }).click();
    await expect(page.getByRole('heading', { name: 'Providers', level: 1 })).toBeVisible();
  });

  test('risk limits require notional currency before save', async ({ page }) => {
    await page.goto('/risk');
    await page.getByRole('button', { name: 'Edit Limits' }).click();

    const notionalInput = page.getByRole('textbox', { name: 'Notional Currency' });
    const originalValue = await notionalInput.inputValue();

    await notionalInput.fill('');
    await page.getByRole('button', { name: 'Save Changes' }).click();

    await expect(page.getByRole('alert').filter({ hasText: 'notionalCurrency required' })).toBeVisible();
    await expect(page.getByText('Save failed').first()).toBeVisible();
    const dismissButtons = page.getByRole('button', { name: 'Dismiss' });
    if (await dismissButtons.count()) {
      await dismissButtons.first().click();
    }

    if (originalValue) {
      await notionalInput.fill(originalValue);
    }
    await page.getByRole('button', { name: 'Cancel' }).click();
    if (originalValue) {
      await expect(page.getByText(originalValue)).toBeVisible();
    }
  });

  test('outbox filters avoid console errors (known issue)', async ({ page }) => {
    const consoleErrors: string[] = [];
    page.on('console', (message) => {
      if (message.type() === 'error') {
        consoleErrors.push(message.text());
      }
    });

    test.fail(true, 'Outbox checkbox still wired to onCheckedChange causing runtime console errors.');

    await page.goto('/outbox');
    const deliveredToggle = page.getByRole('checkbox', { name: /Show delivered/i });
    await expect(deliveredToggle).toBeVisible();
    await deliveredToggle.check({ force: true });
    await expect(deliveredToggle).toBeChecked();

    try {
      await page.getByRole('button', { name: /Collapse issues badge/i }).click({ timeout: 2000 });
    } catch {
      // Overlay did not appear; nothing to collapse.
    }

    await expect(consoleErrors, 'Outbox interactions should not emit console errors').toHaveLength(0);
  });
});
