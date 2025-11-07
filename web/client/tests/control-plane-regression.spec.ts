import { expect, test } from '@playwright/test';

type NavExpectation = {
  linkText: string;
  heading: string;
  path: string;
};

const navExpectations: NavExpectation[] = [
  { linkText: 'Dashboard', heading: 'Dashboard', path: '/' },
  { linkText: 'Instances', heading: 'Strategy Instances', path: '/instances' },
  { linkText: 'Strategies', heading: 'Strategies', path: '/strategies' },
  { linkText: 'Strategy Modules', heading: 'Strategy Modules', path: '/strategies/modules' },
  { linkText: 'Providers', heading: 'Providers', path: '/providers' },
  { linkText: 'Adapters', heading: 'Adapters', path: '/adapters' },
  { linkText: 'Risk Limits', heading: 'Risk Limits', path: '/risk' },
  { linkText: 'Context Backup', heading: 'Context Backup', path: '/context/backup' },
  { linkText: 'Outbox', heading: 'Outbox', path: '/outbox' },
];

const pathToRegex = (path: string) => {
  if (path === '/') {
    return /\/$/;
  }

  const escaped = path.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  return new RegExp(`${escaped}$`);
};

test.describe('control plane regression', () => {
  test('top navigation renders expected sections', async ({ page }) => {
    await page.goto('/');

    for (const { linkText, heading, path } of navExpectations) {
      await page.getByRole('link', { name: linkText, exact: true }).click();
      await expect(page).toHaveURL(pathToRegex(path));
      await expect(page.getByRole('heading', { name: heading, level: 1 })).toBeVisible();
    }
  });

  test('instance creation enforces required fields', async ({ page }) => {
    await page.goto('/instances');
    await page.getByRole('button', { name: 'Create Instance' }).click();
    await page.getByRole('button', { name: 'Create' }).click();

    await expect(page.getByText('Instance ID is required.')).toBeVisible();

    await page.getByRole('tab', { name: 'Guided form' }).click();
    await expect(page.getByRole('checkbox', { name: /binance-demo/i })).toBeVisible();
    await page.getByRole('button', { name: 'Cancel' }).click();
  });

  test('providers start without schema errors', async ({ page }) => {
    await page.goto('/providers');
    await expect(page.getByRole('heading', { name: 'Providers' })).toBeVisible();
    const firstStartButton = page.getByRole('button', { name: /^Start$/ }).first();
    await expect(firstStartButton).toBeEnabled();
    await firstStartButton.click();
    await expect(page.getByText(/Start failed/i)).not.toBeVisible();
  });

  test('risk limits reject negative values', async ({ page }) => {
    await page.goto('/risk');
    await page.getByRole('button', { name: 'Edit Limits' }).click();

    const maxPositionInput = page.getByLabel('Max Position Size');
    const originalValue = await maxPositionInput.inputValue();

    await maxPositionInput.fill('-5');
    await page.getByRole('button', { name: 'Save Changes' }).click();
    await expect(page.getByRole('alert').getByText(/maxPositionSize must be greater than 0/i)).toBeVisible();

    await maxPositionInput.fill(originalValue || '250');
    await page.getByRole('button', { name: 'Save Changes' }).click();
    await expect(page.getByText('Risk limits updated successfully')).toBeVisible();
  });
});
