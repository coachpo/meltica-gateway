import {
  Strategy,
  StrategyModuleSummary,
  StrategyModulePayload,
  StrategyModuleOperationResponse,
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
  StrategyDiagnostic,
  StrategyErrorResponse,
  StrategyValidationErrorResponse,
  ConfigBackup,
  RestoreConfigResponse,
  ContextBackupPayload,
  RestoreContextResponse,
  StrategyRefreshResponse,
  StrategyModulesResponse,
  StrategyModuleUsageResponse,
  StrategyRefreshRequest,
  StrategyRegistryExport,
  OrderHistoryResponse,
  ExecutionHistoryResponse,
  BalanceHistoryResponse,
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

export class StrategyValidationError extends Error {
  readonly diagnostics: StrategyDiagnostic[];
  readonly response: StrategyValidationErrorResponse | StrategyErrorResponse | null;
  readonly status: number;

  constructor(
    message: string,
    diagnostics: StrategyDiagnostic[] = [],
    response: StrategyValidationErrorResponse | StrategyErrorResponse | null,
    status: number,
  ) {
    super(message);
    this.name = 'StrategyValidationError';
    this.diagnostics = diagnostics;
    this.response = response;
    this.status = status;
  }
}

class ApiClient {
  private parseErrorPayload(payload: unknown): StrategyErrorResponse | null {
    if (!payload || typeof payload !== 'object') {
      return null;
    }
    const record = payload as Record<string, unknown>;
    const diagnostics = Array.isArray(record.diagnostics)
      ? record.diagnostics
          .map((entry) => {
            if (!entry || typeof entry !== 'object') {
              return null;
            }
            const diagRecord = entry as Record<string, unknown>;
            const stage = typeof diagRecord.stage === 'string' ? diagRecord.stage : '';
            const message =
              typeof diagRecord.message === 'string' ? diagRecord.message : '';
            if (!stage && !message) {
              return null;
            }
            const line =
              typeof diagRecord.line === 'number'
                ? diagRecord.line
                : Number.isFinite(diagRecord.line)
                  ? Number(diagRecord.line)
                  : undefined;
            const column =
              typeof diagRecord.column === 'number'
                ? diagRecord.column
                : Number.isFinite(diagRecord.column)
                  ? Number(diagRecord.column)
                  : undefined;
            const hint =
              typeof diagRecord.hint === 'string' ? diagRecord.hint : undefined;
            return {
              stage,
              message,
              ...(line !== undefined ? { line } : {}),
              ...(column !== undefined ? { column } : {}),
              ...(hint ? { hint } : {}),
            };
          })
          .filter((entry): entry is StrategyDiagnostic => Boolean(entry))
      : [];
    const message =
      typeof record.message === 'string' && record.message.trim().length > 0
        ? record.message
        : undefined;
    const error =
      typeof record.error === 'string' && record.error.trim().length > 0
        ? record.error
        : undefined;
    return {
      status:
        typeof record.status === 'string' && record.status.trim().length > 0
          ? record.status
          : undefined,
      error: error ?? 'request_failed',
      message,
      diagnostics,
    };
  }

  private raiseError(
    response: Response,
    responseText: string,
  ): never {
    if (response.status === 204) {
      throw new Error('Request failed');
    }
    if (!responseText) {
      throw new Error(`Request failed with status ${response.status}`);
    }
    try {
      const payload = JSON.parse(responseText) as unknown;
      const parsed = this.parseErrorPayload(payload);
      if (response.status === 422 && parsed?.error === 'strategy_validation_failed') {
        const message =
          parsed.message && parsed.message.trim().length > 0
            ? parsed.message
            : 'Strategy module validation failed';
        throw new StrategyValidationError(
          message,
          parsed.diagnostics ?? [],
          parsed as StrategyValidationErrorResponse,
          response.status,
        );
      }
      if (parsed) {
        const message =
          parsed.message && parsed.message.trim().length > 0
            ? parsed.message
            : parsed.error && parsed.error.trim().length > 0
              ? parsed.error
              : `Request failed with status ${response.status}`;
        throw new Error(message);
      }
    } catch (err) {
      if (err instanceof StrategyValidationError) {
        throw err;
      }
      // fall back to raw text if JSON parsing failed
    }
    throw new Error(responseText);
  }

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
      this.raiseError(response, responseText);
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

  private async requestText(
    endpoint: string,
    options: RequestInit = {}
  ): Promise<string> {
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
      this.raiseError(response, responseText);
    }

    return responseText;
  }

  // Strategy Catalog
  async getStrategies(): Promise<{ strategies: Strategy[] }> {
    return this.request('/strategies');
  }

  async getStrategy(name: string): Promise<Strategy> {
    return this.request(`/strategies/${encodeURIComponent(name)}`);
  }

  // Strategy Modules
  async getStrategyModules(params?: {
    strategy?: string;
    hash?: string;
    runningOnly?: boolean;
    limit?: number;
    offset?: number;
  }): Promise<StrategyModulesResponse> {
    let path = '/strategies/modules';
    if (params) {
      const searchParams = new URLSearchParams();
      if (params.strategy) {
        searchParams.set('strategy', params.strategy);
      }
      if (params.hash) {
        searchParams.set('hash', params.hash);
      }
      if (params.runningOnly !== undefined) {
        searchParams.set('runningOnly', String(params.runningOnly));
      }
      if (typeof params.limit === 'number' && params.limit >= 0) {
        searchParams.set('limit', String(params.limit));
      }
      if (typeof params.offset === 'number' && params.offset >= 0) {
        searchParams.set('offset', String(params.offset));
      }
      const query = searchParams.toString();
      if (query) {
        path = `${path}?${query}`;
      }
    }
    return this.request(path);
  }

  async getStrategyModule(identifier: string): Promise<StrategyModuleSummary> {
    return this.request(`/strategies/modules/${encodeURIComponent(identifier)}`);
  }

  async getStrategyModuleUsage(
    selector: string,
    params?: { limit?: number; offset?: number; includeStopped?: boolean }
  ): Promise<StrategyModuleUsageResponse> {
    let path = `/strategies/modules/${encodeURIComponent(selector)}/usage`;
    if (params) {
      const searchParams = new URLSearchParams();
      if (typeof params.limit === 'number' && params.limit >= 0) {
        searchParams.set('limit', String(params.limit));
      }
      if (typeof params.offset === 'number' && params.offset >= 0) {
        searchParams.set('offset', String(params.offset));
      }
      if (params.includeStopped !== undefined) {
        searchParams.set('includeStopped', String(params.includeStopped));
      }
      const query = searchParams.toString();
      if (query) {
        path = `${path}?${query}`;
      }
    }
    return this.request(path);
  }

  async getStrategyModuleSource(identifier: string): Promise<string> {
    return this.requestText(`/strategies/modules/${encodeURIComponent(identifier)}/source`);
  }

  async createStrategyModule(
    payload: StrategyModulePayload
  ): Promise<StrategyModuleOperationResponse> {
    return this.request('/strategies/modules', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async updateStrategyModule(
    identifier: string,
    payload: StrategyModulePayload
  ): Promise<StrategyModuleOperationResponse> {
    return this.request(`/strategies/modules/${encodeURIComponent(identifier)}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    });
  }

  async deleteStrategyModule(identifier: string): Promise<void> {
    await this.request(`/strategies/modules/${encodeURIComponent(identifier)}`, {
      method: 'DELETE',
    });
  }

  async refreshStrategies(payload?: StrategyRefreshRequest): Promise<StrategyRefreshResponse> {
    return this.request('/strategies/refresh', {
      method: 'POST',
      body: payload ? JSON.stringify(payload) : undefined,
    });
  }

  async exportStrategyRegistry(): Promise<StrategyRegistryExport> {
    return this.request('/strategies/registry');
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

  async getInstanceOrders(
    id: string,
    params?: { limit?: number; provider?: string; states?: string[] }
  ): Promise<OrderHistoryResponse> {
    const search = new URLSearchParams();
    if (params?.limit && params.limit > 0) {
      search.set('limit', String(params.limit));
    }
    const provider = params?.provider?.trim();
    if (provider) {
      search.set('provider', provider);
    }
    if (params?.states?.length) {
      params.states.forEach((state) => {
        const trimmed = state.trim();
        if (trimmed) {
          search.append('state', trimmed);
        }
      });
    }
    const query = search.toString();
    const path = `/strategy/instances/${encodeURIComponent(id)}/orders${query ? `?${query}` : ''}`;
    return this.request(path);
  }

  async getInstanceExecutions(
    id: string,
    params?: { limit?: number; provider?: string; orderId?: string }
  ): Promise<ExecutionHistoryResponse> {
    const search = new URLSearchParams();
    if (params?.limit && params.limit > 0) {
      search.set('limit', String(params.limit));
    }
    const provider = params?.provider?.trim();
    if (provider) {
      search.set('provider', provider);
    }
    const orderId = params?.orderId?.trim();
    if (orderId) {
      search.set('orderId', orderId);
    }
    const query = search.toString();
    const path = `/strategy/instances/${encodeURIComponent(id)}/executions${query ? `?${query}` : ''}`;
    return this.request(path);
  }

  async getProviderBalances(
    name: string,
    params?: { limit?: number; asset?: string }
  ): Promise<BalanceHistoryResponse> {
    const search = new URLSearchParams();
    if (params?.limit && params.limit > 0) {
      search.set('limit', String(params.limit));
    }
    const asset = params?.asset?.trim();
    if (asset) {
      search.set('asset', asset);
    }
    const query = search.toString();
    const path = `/providers/${encodeURIComponent(name)}/balances${query ? `?${query}` : ''}`;
    return this.request(path);
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

  async getContextBackup(): Promise<ContextBackupPayload> {
    return this.request('/context/backup');
  }

  async restoreContextBackup(payload: ContextBackupPayload): Promise<RestoreContextResponse> {
    return this.request('/context/backup', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }
}

export const apiClient = new ApiClient();
export { StrategyValidationError };
