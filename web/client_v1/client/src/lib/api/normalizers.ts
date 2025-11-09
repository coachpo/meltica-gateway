import type { RiskConfig, RuntimeConfig, RuntimeConfigSnapshot, RuntimeConfigSource } from '@/lib/types';

type PartialRiskConfigResponse = Partial<Omit<RiskConfig, 'circuitBreaker'>> & {
  circuitBreaker?: Partial<RiskConfig['circuitBreaker']>;
};

const runtimeKeys: Array<keyof RuntimeConfig> = ['eventbus', 'pools', 'risk', 'apiServer', 'telemetry'];

function isRuntimeConfig(value: unknown): value is RuntimeConfig {
  if (!value || typeof value !== 'object') {
    return false;
  }
  const record = value as Record<string, unknown>;
  return runtimeKeys.every((key) => Object.prototype.hasOwnProperty.call(record, key));
}

function pickValue(source: Record<string, unknown>, keys: string[]): unknown {
  for (const key of keys) {
    if (Object.prototype.hasOwnProperty.call(source, key)) {
      return source[key];
    }
  }
  return undefined;
}

function toStringValue(value: unknown): string | undefined {
  if (value === undefined || value === null) {
    return undefined;
  }
  const stringified = String(value).trim();
  return stringified || undefined;
}

function toNumberValue(value: unknown): number | undefined {
  if (value === undefined || value === null) {
    return undefined;
  }
  const candidate = typeof value === 'number' ? value : Number(String(value).trim());
  return Number.isFinite(candidate) ? candidate : undefined;
}

function toBooleanValue(value: unknown): boolean | undefined {
  if (value === undefined || value === null) {
    return undefined;
  }
  if (typeof value === 'boolean') {
    return value;
  }
  if (typeof value === 'number') {
    return value !== 0;
  }
  if (typeof value === 'string') {
    const normalized = value.trim().toLowerCase();
    if (!normalized) {
      return undefined;
    }
    if (['true', '1', 'yes', 'on', 'enabled'].includes(normalized)) {
      return true;
    }
    if (['false', '0', 'no', 'off', 'disabled'].includes(normalized)) {
      return false;
    }
  }
  return undefined;
}

function toStringArray(value: unknown): string[] | undefined {
  if (!value) {
    return undefined;
  }
  if (Array.isArray(value)) {
    const items = value
      .map((entry) => String(entry).trim())
      .filter((entry) => entry.length > 0);
    return items.length > 0 ? items : undefined;
  }
  if (typeof value === 'string') {
    const items = value
      .split(',')
      .map((entry) => entry.trim())
      .filter((entry) => entry.length > 0);
    return items.length > 0 ? items : undefined;
  }
  return undefined;
}

export function normalizeRiskLimitsResponse(payload: unknown): PartialRiskConfigResponse {
  const source =
    payload && typeof payload === 'object'
      ? (payload as Record<string, unknown>)
      : {};

  const result: PartialRiskConfigResponse = {};

  const maxPositionSize = toStringValue(pickValue(source, ['maxPositionSize', 'MaxPositionSize']));
  if (maxPositionSize !== undefined) {
    result.maxPositionSize = maxPositionSize;
  }

  const maxNotionalValue = toStringValue(pickValue(source, ['maxNotionalValue', 'MaxNotionalValue']));
  if (maxNotionalValue !== undefined) {
    result.maxNotionalValue = maxNotionalValue;
  }

  const notionalCurrency = toStringValue(pickValue(source, ['notionalCurrency', 'NotionalCurrency']));
  if (notionalCurrency !== undefined) {
    result.notionalCurrency = notionalCurrency;
  }

  const orderThrottle = toNumberValue(pickValue(source, ['orderThrottle', 'OrderThrottle']));
  if (orderThrottle !== undefined) {
    result.orderThrottle = orderThrottle;
  }

  const orderBurst = toNumberValue(pickValue(source, ['orderBurst', 'OrderBurst']));
  if (orderBurst !== undefined) {
    result.orderBurst = orderBurst;
  }

  const maxConcurrentOrders = toNumberValue(pickValue(source, ['maxConcurrentOrders', 'MaxConcurrentOrders']));
  if (maxConcurrentOrders !== undefined) {
    result.maxConcurrentOrders = maxConcurrentOrders;
  }

  const priceBandPercent = toNumberValue(pickValue(source, ['priceBandPercent', 'PriceBandPercent']));
  if (priceBandPercent !== undefined) {
    result.priceBandPercent = priceBandPercent;
  }

  const allowedOrderTypes = toStringArray(pickValue(source, ['allowedOrderTypes', 'AllowedOrderTypes']));
  if (allowedOrderTypes !== undefined) {
    result.allowedOrderTypes = allowedOrderTypes;
  }

  const killSwitchEnabled = toBooleanValue(pickValue(source, ['killSwitchEnabled', 'KillSwitchEnabled']));
  if (killSwitchEnabled !== undefined) {
    result.killSwitchEnabled = killSwitchEnabled;
  }

  const maxRiskBreaches = toNumberValue(pickValue(source, ['maxRiskBreaches', 'MaxRiskBreaches']));
  if (maxRiskBreaches !== undefined) {
    result.maxRiskBreaches = maxRiskBreaches;
  }

  const circuitRaw = pickValue(source, ['circuitBreaker', 'CircuitBreaker']);
  if (circuitRaw && typeof circuitRaw === 'object') {
    const circuitSource = circuitRaw as Record<string, unknown>;
    const circuit: Partial<RiskConfig['circuitBreaker']> = {};

    const enabled = toBooleanValue(pickValue(circuitSource, ['enabled', 'Enabled']));
    if (enabled !== undefined) {
      circuit.enabled = enabled;
    }

    const threshold = toNumberValue(pickValue(circuitSource, ['threshold', 'Threshold']));
    if (threshold !== undefined) {
      circuit.threshold = threshold;
    }

    const cooldown = toStringValue(pickValue(circuitSource, ['cooldown', 'Cooldown']));
    if (cooldown !== undefined) {
      circuit.cooldown = cooldown;
    }

    if (Object.keys(circuit).length > 0) {
      result.circuitBreaker = circuit;
    }
  }

  return result;
}

export function serializeRiskLimitsPayload(config: RiskConfig): Record<string, unknown> {
  return {
    MaxPositionSize: config.maxPositionSize,
    MaxNotionalValue: config.maxNotionalValue,
    NotionalCurrency: config.notionalCurrency,
    OrderThrottle: config.orderThrottle,
    OrderBurst: config.orderBurst,
    MaxConcurrentOrders: config.maxConcurrentOrders,
    PriceBandPercent: config.priceBandPercent,
    AllowedOrderTypes: config.allowedOrderTypes,
    KillSwitchEnabled: config.killSwitchEnabled,
    MaxRiskBreaches: config.maxRiskBreaches,
    CircuitBreaker: {
      Enabled: config.circuitBreaker?.enabled ?? false,
      Threshold: config.circuitBreaker?.threshold ?? 0,
      Cooldown: config.circuitBreaker?.cooldown ?? '',
    },
  };
}

export function normalizeRuntimeConfigSnapshot(payload: unknown): RuntimeConfigSnapshot {
  if (!payload) {
    throw new Error('Empty runtime configuration payload');
  }
  if (isRuntimeConfig(payload)) {
    return {
      config: payload,
      source: 'runtime',
    };
  }

  if (typeof payload !== 'object') {
    throw new Error('Malformed runtime configuration payload');
  }

  const data = payload as Record<string, unknown>;
  const configCandidate = [data.config, data.runtime].find(isRuntimeConfig);

  if (!configCandidate) {
    throw new Error('Runtime configuration missing from response');
  }

  const sourceRaw =
    typeof data.source === 'string' ? (data.source as RuntimeConfigSource) : undefined;
  const source: RuntimeConfigSource = ['runtime', 'file', 'bootstrap'].includes(String(sourceRaw))
    ? (sourceRaw as RuntimeConfigSource)
    : 'runtime';

  const persistedAt =
    typeof data.persistedAt === 'string'
      ? (data.persistedAt as string)
      : typeof data.persisted_at === 'string'
        ? (data.persisted_at as string)
        : null;

  const filePath =
    typeof data.filePath === 'string'
      ? (data.filePath as string)
      : typeof data.path === 'string'
        ? (data.path as string)
        : null;

  const metadata =
    data.metadata && typeof data.metadata === 'object'
      ? (data.metadata as Record<string, unknown>)
      : null;

  return {
    config: configCandidate,
    source,
    persistedAt,
    filePath,
    metadata,
  };
}

export type { PartialRiskConfigResponse };
