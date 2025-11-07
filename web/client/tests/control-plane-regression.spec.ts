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

  test('runtime config endpoint responds with data (known issue)', async ({ page }) => {
    test.fail(true, 'Control API currently returns 404 for /config/runtime');
    const runtimeResponsePromise = page.waitForResponse((response) =>
      response.url().includes('://localhost:8880/config/runtime') && response.request().method() === 'GET'
    );
    await page.goto('/config/runtime');
    const runtimeResponse = await runtimeResponsePromise;
    expect(runtimeResponse.status(), 'control API should respond 200 for /config/runtime').toBe(200);
  });

  test('config backup endpoint responds with data (known issue)', async ({ page }) => {
    test.fail(true, 'Control API currently returns 404 for /config/backup');
    const backupResponsePromise = page.waitForResponse((response) =>
      response.url().includes('://localhost:8880/config/backup') && response.request().method() === 'GET'
    );
    await page.goto('/config/backup');
    const backupResponse = await backupResponsePromise;
    expect(backupResponse.status(), 'control API should respond 200 for /config/backup').toBe(200);
  });

  test('providers start without schema errors (known issue)', async ({ page }) => {
    test.fail(true, 'Starting binance-demo currently throws a ZodError for instruments=null');
    await page.goto('/providers');
    await expect(page.getByRole('heading', { name: 'Providers' })).toBeVisible();
    const errorToast = page.getByText(/Start failed/i);
    const firstStartButton = page.getByRole('button', { name: /^Start$/ }).first();
    await expect(firstStartButton).toBeEnabled();
    await firstStartButton.click();
    await expect(errorToast).not.toBeVisible();
  });

  test('risk limits reject negative values (known issue)', async ({ page }) => {
    test.fail(true, 'Risk limit form currently persists negative values');
    await page.goto('/risk');
    await page.getByRole('button', { name: 'Edit Limits' }).click();

    const maxPositionInput = page.getByLabel('Max Position Size');
    const originalValue = await maxPositionInput.inputValue();

    try {
      await maxPositionInput.fill('-5');
      await page.getByRole('button', { name: 'Save Changes' }).click();
      await expect(page.getByText(/must be greater than 0/i)).toBeVisible();
    } finally {
      await page.getByRole('button', { name: 'Edit Limits' }).click();
      await page.getByLabel('Max Position Size').fill(originalValue || '250');
      await page.getByRole('button', { name: 'Save Changes' }).click();
    }
  });
});
