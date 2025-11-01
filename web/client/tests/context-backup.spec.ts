import { expect, test } from '@playwright/test';
import { sanitizeContextBackupPayload, getSensitiveKeyFragments } from '../src/lib/context-backup';

test('sanitizes sensitive keys recursively without mutating input', () => {
  const payload = {
    providers: [
      {
        Name: 'alpha',
        Config: {
          api_key: 'should-be-removed',
          secret: 'also-removed',
          nested: {
            token: 'remove-me',
            keep: 'ok',
          },
        },
      },
    ],
    lambdas: [
      {
        id: 'lambda-1',
        strategy: {
          config: {
            secret_token: 'strip',
            ttl: 1,
          },
        },
      },
    ],
    risk: {
      MaxPositionSize: '250',
      passphrase: 'remove',
      nested: {
        apiKey: 'remove',
        control: true,
      },
    },
  } satisfies Record<string, unknown>;

  const sanitized = sanitizeContextBackupPayload(payload);

  expect(sanitized.providers).toHaveLength(1);
  expect(sanitized.providers[0].Config).toBeDefined();
  expect((sanitized.providers[0].Config as Record<string, unknown>).api_key).toBeUndefined();
  expect((sanitized.providers[0].Config as Record<string, unknown>).secret).toBeUndefined();
  expect(
    ((sanitized.providers[0].Config as Record<string, unknown>).nested as Record<string, unknown>).token
  ).toBeUndefined();
  expect(
    ((sanitized.providers[0].Config as Record<string, unknown>).nested as Record<string, unknown>).keep
  ).toBe('ok');

  expect(sanitized.lambdas[0].strategy).toBeDefined();
  expect(
    ((sanitized.lambdas[0].strategy as Record<string, unknown>).config as Record<string, unknown>).secret_token
  ).toBeUndefined();
  expect(
    ((sanitized.lambdas[0].strategy as Record<string, unknown>).config as Record<string, unknown>).ttl
  ).toBe(1);

  expect(sanitized.risk.MaxPositionSize).toBe('250');
  expect((sanitized.risk as Record<string, unknown>).passphrase).toBeUndefined();
  expect(
    ((sanitized.risk as Record<string, unknown>).nested as Record<string, unknown>).control
  ).toBe(true);
});

test('throws when required collections are missing', () => {
  expect(() => sanitizeContextBackupPayload({})).toThrow(/providers/i);
  expect(() => sanitizeContextBackupPayload({ providers: [] })).toThrow(/lambdas/i);
  expect(() => sanitizeContextBackupPayload({ providers: [], lambdas: [] })).toThrow(/risk/i);
});

test('sensitive key fragments list includes documented patterns', () => {
  const fragments = getSensitiveKeyFragments();
  expect(fragments).toEqual(expect.arrayContaining(['api_key', 'secret', 'token']));
});
