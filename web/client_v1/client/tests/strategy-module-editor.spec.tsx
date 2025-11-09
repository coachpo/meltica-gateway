import { expect, test } from '@playwright/test';
import {
  STRATEGY_MODULE_TEMPLATE,
  nextValidationFeedbackAfterEdit,
} from '../src/app/strategies/modules/page';
import type { StrategyDiagnostic } from '../src/lib/types';

test('strategy template includes metadata scaffolding', () => {
  expect(STRATEGY_MODULE_TEMPLATE).toContain('metadata');
  expect(STRATEGY_MODULE_TEMPLATE).toContain('displayName');
  expect(STRATEGY_MODULE_TEMPLATE).toContain('events');
  expect(STRATEGY_MODULE_TEMPLATE).toContain('config');
});

test('strategy module editor renders code editor wrapper when enhanced mode disabled', async ({ page }) => {
  await page.goto('http://localhost:3000/strategies/modules');
  await page.getByRole('button', { name: 'New module' }).click();
  await expect(page.locator('[data-slot="code-editor"]')).toBeVisible();
});

test('nextValidationFeedbackAfterEdit clears diagnostics and errors', () => {
  const diagnostics: StrategyDiagnostic[] = [{ stage: 'validation', message: 'displayName required' }];
  const result = nextValidationFeedbackAfterEdit(diagnostics, 'Metadata validation failed');
  expect(result.diagnostics.length).toBe(0);
  expect(result.error).toBeNull();
  const emptyDiagnostics: StrategyDiagnostic[] = [];
  const identity = nextValidationFeedbackAfterEdit(emptyDiagnostics, null);
  expect(identity.diagnostics).toBe(emptyDiagnostics);
  expect(identity.error).toBeNull();
});
