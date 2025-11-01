import {
  Strategy,
  Provider,
  ProviderDetail,
  ProviderRequest,
  AdapterMetadata,
  InstanceSummary,
  InstanceSpec,
  RiskConfig,
  RuntimeConfig,
  RuntimeConfigSnapshot,
  RuntimeConfigSource,
  ApiError,
  ConfigBackup,
  RestoreConfigResponse,
} from './types';

const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8880';

const isRuntimeConfig = (value: unknown): value is RuntimeConfig =>
  Boolean(
    value &&
      typeof value === 'object' &&
      'eventbus' in (value as Record<string, unknown>) &&
      'pools' in (value as Record<string, unknown>) &&
      'risk' in (value as Record<string, unknown>) &&
      'apiServer' in (value as Record<string, unknown>) &&
      'telemetry' in (value as Record<string, unknown>)
  );

const normaliseRuntimeConfigSnapshot = (payload: unknown): RuntimeConfigSnapshot => {
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

  const sourceRaw = typeof data.source === 'string' ? (data.source as RuntimeConfigSource) : undefined;
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
};

type PartialRiskConfigResponse = Partial<Omit<RiskConfig, 'circuitBreaker'>> & {
  circuitBreaker?: Partial<RiskConfig['circuitBreaker']>;
};

const pickValue = (source: Record<string, unknown>, keys: string[]): unknown => {
  for (const key of keys) {
    if (Object.prototype.hasOwnProperty.call(source, key)) {
      return source[key];
    }
  }
  return undefined;
};

const toStringValue = (value: unknown): string | undefined => {
  if (value === undefined || value === null) {
    return undefined;
  }
  const stringified = String(value).trim();
  return stringified ? stringified : undefined;
};

const toNumberValue = (value: unknown): number | undefined => {
  if (value === undefined || value === null) {
    return undefined;
  }
  const candidate = typeof value === 'number' ? value : Number(String(value).trim());
  return Number.isFinite(candidate) ? candidate : undefined;
};

const toBooleanValue = (value: unknown): boolean | undefined => {
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
    const normalised = value.trim().toLowerCase();
    if (!normalised) {
      return undefined;
    }
    if (['true', '1', 'yes', 'on', 'enabled'].includes(normalised)) {
      return true;
    }
    if (['false', '0', 'no', 'off', 'disabled'].includes(normalised)) {
      return false;
    }
  }
  return undefined;
};

const toStringArray = (value: unknown): string[] | undefined => {
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
};

const normaliseRiskLimitsResponse = (payload: unknown): PartialRiskConfigResponse => {
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
};

const serialiseRiskLimitsPayload = (config: RiskConfig): Record<string, unknown> => ({
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
});

class ApiClient {
  private async request<T>(
    endpoint: string,
    options: RequestInit = {}
  ): Promise<T> {
    const url = `${API_BASE_URL}${endpoint}`;
    const response = await fetch(url, {
      ...options,
      headers: {
        'Content-Type': 'application/json',
        ...options.headers,
      },
    });

    const responseText = await response.text();

    if (!response.ok) {
      let message = 'Request failed';
      if (responseText) {
        try {
          const error: ApiError = JSON.parse(responseText);
          message = error.error || message;
        } catch {
          message = responseText;
        }
      }
      throw new Error(message);
    }

    if (!responseText) {
      return undefined as T;
    }

    try {
      return JSON.parse(responseText) as T;
    } catch {
      throw new Error('Invalid JSON response');
    }
  }

  // Strategy Catalog
  async getStrategies(): Promise<{ strategies: Strategy[] }> {
    return this.request('/strategies');
  }

  async getStrategy(name: string): Promise<Strategy> {
    return this.request(`/strategies/${encodeURIComponent(name)}`);
  }

  // Providers
  async getProviders(): Promise<{ providers: Provider[] }> {
    return this.request('/providers');
  }

  async getProvider(name: string): Promise<ProviderDetail> {
    return this.request(`/providers/${encodeURIComponent(name)}`);
  }

  async createProvider(payload: ProviderRequest): Promise<ProviderDetail> {
    return this.request('/providers', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async updateProvider(name: string, payload: ProviderRequest): Promise<ProviderDetail> {
    return this.request(`/providers/${encodeURIComponent(name)}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    });
  }

  async deleteProvider(name: string): Promise<{ status: string; name: string }> {
    return this.request(`/providers/${encodeURIComponent(name)}`, {
      method: 'DELETE',
    });
  }

  async startProvider(name: string): Promise<ProviderDetail> {
    return this.request(`/providers/${encodeURIComponent(name)}/start`, {
      method: 'POST',
    });
  }

  async stopProvider(name: string): Promise<ProviderDetail> {
    return this.request(`/providers/${encodeURIComponent(name)}/stop`, {
      method: 'POST',
    });
  }

  // Adapters
  async getAdapters(): Promise<{ adapters: AdapterMetadata[] }> {
    return this.request('/adapters');
  }

  async getAdapter(identifier: string): Promise<AdapterMetadata> {
    return this.request(`/adapters/${encodeURIComponent(identifier)}`);
  }

  // Strategy Instances
  async getInstances(): Promise<{ instances: InstanceSummary[] }> {
    return this.request('/strategy/instances');
  }

  async getInstance(id: string): Promise<InstanceSpec> {
    return this.request(`/strategy/instances/${encodeURIComponent(id)}`);
  }

  async createInstance(spec: InstanceSpec): Promise<InstanceSpec> {
    return this.request('/strategy/instances', {
      method: 'POST',
      body: JSON.stringify(spec),
    });
  }

  async updateInstance(id: string, spec: InstanceSpec): Promise<InstanceSpec> {
    return this.request(`/strategy/instances/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(spec),
    });
  }

  async deleteInstance(id: string): Promise<{ status: string; id: string }> {
    return this.request(`/strategy/instances/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    });
  }

  async startInstance(id: string): Promise<InstanceSpec> {
    return this.request(`/strategy/instances/${encodeURIComponent(id)}/start`, {
      method: 'POST',
    });
  }

  async stopInstance(id: string): Promise<InstanceSpec> {
    return this.request(`/strategy/instances/${encodeURIComponent(id)}/stop`, {
      method: 'POST',
    });
  }

  // Risk Limits
  async getRiskLimits(): Promise<{ status?: string; limits: PartialRiskConfigResponse }> {
    const payload = await this.request<Record<string, unknown>>('/risk/limits');
    const rawLimits =
      payload && typeof payload === 'object' && Object.prototype.hasOwnProperty.call(payload, 'limits')
        ? (payload.limits as unknown)
        : payload;
    const status = typeof payload.status === 'string' ? payload.status : undefined;
    const limits = normaliseRiskLimitsResponse(rawLimits);
    return status ? { status, limits } : { limits };
  }

  async updateRiskLimits(
    config: RiskConfig
  ): Promise<{ status?: string; limits: PartialRiskConfigResponse }> {
    const payload = await this.request<Record<string, unknown>>('/risk/limits', {
      method: 'PUT',
      body: JSON.stringify(serialiseRiskLimitsPayload(config)),
    });
    const rawLimits =
      payload && typeof payload === 'object' && Object.prototype.hasOwnProperty.call(payload, 'limits')
        ? (payload.limits as unknown)
        : payload;
    const status = typeof payload.status === 'string' ? payload.status : undefined;
    const limits = normaliseRiskLimitsResponse(rawLimits);
    return status ? { status, limits } : { limits };
  }

  async getRuntimeConfig(): Promise<RuntimeConfigSnapshot> {
    const payload = await this.request<unknown>('/config/runtime');
    return normaliseRuntimeConfigSnapshot(payload);
  }

  async updateRuntimeConfig(payload: RuntimeConfig): Promise<RuntimeConfigSnapshot> {
    const response = await this.request<unknown>('/config/runtime', {
      method: 'PUT',
      body: JSON.stringify(payload),
    });
    if (response === undefined) {
      const fallback = await this.request<unknown>('/config/runtime');
      return normaliseRuntimeConfigSnapshot(fallback);
    }
    return normaliseRuntimeConfigSnapshot(response);
  }

  async revertRuntimeConfig(): Promise<RuntimeConfigSnapshot> {
    const response = await this.request<unknown>('/config/runtime', {
      method: 'DELETE',
    });
    if (response === undefined) {
      const fallback = await this.request<unknown>('/config/runtime');
      return normaliseRuntimeConfigSnapshot(fallback);
    }
    return normaliseRuntimeConfigSnapshot(response);
  }

  async getConfigBackup(): Promise<ConfigBackup> {
    return this.request('/config/backup');
  }

  async restoreConfigBackup(payload: ConfigBackup): Promise<RestoreConfigResponse> {
    return this.request('/config/backup', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }
}

export const apiClient = new ApiClient();
