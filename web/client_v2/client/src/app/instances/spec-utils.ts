import type { InstanceSpec } from '@/lib/types';

type ParseOptions = {
  strict?: boolean;
};

export type InstanceSpecParseResult = {
  spec?: InstanceSpec;
  error?: string;
};

const sanitizeString = (value: unknown): string => {
  if (typeof value !== 'string') {
    return '';
  }
  return value.trim();
};

const sanitizeOptionalString = (value: unknown): string | undefined => {
  const trimmed = sanitizeString(value);
  return trimmed ? trimmed : undefined;
};

const isRecord = (value: unknown): value is Record<string, unknown> =>
  Boolean(value) && typeof value === 'object' && !Array.isArray(value);

export const formatInstanceSpec = (spec: InstanceSpec, indent = 2): string => {
  const base: Record<string, unknown> = {
    id: spec.id,
    strategy: {
      identifier: spec.strategy.identifier,
      selector: spec.strategy.selector,
      config: spec.strategy.config ?? {},
    },
    scope: spec.scope,
  };

  if (spec.strategy.tag) {
    (base.strategy as Record<string, unknown>).tag = spec.strategy.tag;
  }
  if (spec.strategy.hash) {
    (base.strategy as Record<string, unknown>).hash = spec.strategy.hash;
  }
  if (Array.isArray(spec.providers) && spec.providers.length > 0) {
    base.providers = [...spec.providers];
  }
  return JSON.stringify(base, null, indent);
};

export const parseInstanceSpecDraft = (
  rawInput: string,
  options?: ParseOptions,
): InstanceSpecParseResult => {
  const strict = options?.strict ?? true;
  const trimmed = rawInput.trim();
  if (!trimmed) {
    return { error: 'Provide an instance specification in JSON format.' };
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch (err) {
    const message =
      err instanceof SyntaxError ? err.message.split('\n')[0] : (err as Error | undefined)?.message;
    return { error: message ? `Invalid JSON: ${message}` : 'Invalid JSON payload' };
  }

  if (!isRecord(parsed)) {
    return { error: 'Instance specification must be a JSON object.' };
  }

  const data = parsed as Record<string, unknown>;
  const id = sanitizeString(data.id);
  if (strict && !id) {
    return { error: 'Instance ID is required.' };
  }

  const strategyRaw = data.strategy;
  if (strict && !isRecord(strategyRaw)) {
    return { error: 'Strategy definition is required.' };
  }

  const strategy = isRecord(strategyRaw) ? strategyRaw : {};
  const identifier = sanitizeString(strategy.identifier);
  if (strict && !identifier) {
    return { error: 'Strategy identifier is required.' };
  }

  const tag = sanitizeOptionalString(strategy.tag);
  const hash = sanitizeOptionalString(strategy.hash);
  let selector = sanitizeString(strategy.selector);
  if (!selector) {
    if (hash) {
      selector = `${identifier || ''}@${hash}`;
    } else if (tag) {
      selector = `${identifier || ''}:${tag}`;
    } else {
      selector = identifier;
    }
  }

  const config =
    isRecord(strategy.config) || Array.isArray(strategy.config) ? strategy.config : {};

  const scopeRaw = data.scope;
  if (strict && !isRecord(scopeRaw)) {
    return { error: 'Scope must map providers to symbol assignments.' };
  }

  const scopeSource = isRecord(scopeRaw) ? scopeRaw : {};
  const scope: InstanceSpec['scope'] = {};
  const aggregatedSymbols = new Set<string>();

  for (const [providerName, assignment] of Object.entries(scopeSource)) {
    const name = providerName.trim();
    if (!name) {
      if (strict) {
        return { error: 'Provider names in scope must be non-empty strings.' };
      }
      // Skip unnamed providers in non-strict mode.
      continue;
    }

    if (!isRecord(assignment) || !Array.isArray(assignment.symbols)) {
      if (strict) {
        return { error: `Scope entry for "${name}" must include a symbols array.` };
      }
      scope[name] = { symbols: [] };
      continue;
    }

    const rawSymbols = assignment.symbols as unknown[];
    const symbols = rawSymbols
      .map((symbol) => (typeof symbol === 'string' ? symbol.trim().toUpperCase() : ''))
      .filter((symbol) => symbol.length > 0);

    if (strict && symbols.length === 0) {
      return { error: `Provider "${name}" requires at least one instrument symbol.` };
    }

    symbols.forEach((symbol) => aggregatedSymbols.add(symbol));
    scope[name] = { symbols };
  }

  if (strict && Object.keys(scope).length === 0) {
    return { error: 'Assign at least one provider with symbols in scope.' };
  }

  const providers = Object.keys(scope);
  const spec: InstanceSpec = {
    id,
    strategy: {
      identifier,
      selector,
      config: config as Record<string, unknown>,
    },
    scope,
  };

  if (tag) {
    spec.strategy.tag = tag;
  }
  if (hash) {
    spec.strategy.hash = hash;
  }

  if (providers.length > 0) {
    spec.providers = providers;
  }
  if (aggregatedSymbols.size > 0) {
    spec.aggregatedSymbols = Array.from(aggregatedSymbols);
  }

  return { spec };
};
