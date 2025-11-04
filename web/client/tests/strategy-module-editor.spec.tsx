import { expect, test } from '@playwright/test';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { StrategyModuleEditor } from '../src/components/strategy-module-editor';
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

test('strategy module editor falls back to textarea when enhanced mode disabled', () => {
  const markup = renderToStaticMarkup(
    <StrategyModuleEditor
      value={'module.exports = {};'}
      onChange={() => {}}
      diagnostics={[]}
      useEnhancedEditor={false}
      aria-label="strategy-source"
    />,
  );
  expect(markup).toContain('textarea');
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
