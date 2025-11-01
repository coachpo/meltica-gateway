import { ContextBackupPayload } from './types';

const SENSITIVE_KEY_FRAGMENTS = ['api_key', 'apikey', 'secret', 'token', 'passphrase', 'password'];

const ensureArray = (value: unknown, context: string): unknown[] => {
  if (!Array.isArray(value)) {
    throw new Error(`${context} must be an array`);
  }
  return value;
};

const normalizeArray = (value: unknown, context: string): unknown[] => {
  if (value === undefined || value === null) {
    return [];
  }
  return ensureArray(value, context);
};

const ensureRecord = (value: unknown, context: string): Record<string, unknown> => {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error(`${context} must be an object`);
  }
  return value as Record<string, unknown>;
};

const sanitizeUnknown = (value: unknown): unknown => {
  if (Array.isArray(value)) {
    return value.map((entry) => sanitizeUnknown(entry));
  }

  if (value && typeof value === 'object') {
    const source = value as Record<string, unknown>;
    const result: Record<string, unknown> = {};
    for (const [key, entry] of Object.entries(source)) {
      const normalisedKey = key.toLowerCase();
      if (SENSITIVE_KEY_FRAGMENTS.some((fragment) => normalisedKey.includes(fragment))) {
        continue;
      }
      result[key] = sanitizeUnknown(entry);
    }
    return result;
  }

  return value;
};

const sanitizeRecord = (value: unknown, context: string): Record<string, unknown> => {
  const source = ensureRecord(value, context);
  const sanitized = sanitizeUnknown(source);
  return ensureRecord(sanitized, context);
};

export const sanitizeContextBackupPayload = (input: unknown): ContextBackupPayload => {
  const source = ensureRecord(input, 'Backup payload');

  const providersRaw = normalizeArray(source.providers, 'Backup payload providers');
  const lambdasRaw = normalizeArray(source.lambdas, 'Backup payload lambdas');
  const riskRaw = sanitizeRecord(source.risk, 'Backup payload risk');

  const providers = providersRaw.map((entry, index) =>
    sanitizeRecord(entry, `Provider entry at index ${index}`)
  );

  const lambdas = lambdasRaw.map((entry, index) =>
    sanitizeRecord(entry, `Lambda entry at index ${index}`)
  );

  return {
    providers,
    lambdas,
    risk: riskRaw,
  };
};

export const formatContextBackupPayload = (payload: ContextBackupPayload): string =>
  JSON.stringify(payload, null, 2);

export const getSensitiveKeyFragments = (): readonly string[] => SENSITIVE_KEY_FRAGMENTS;
