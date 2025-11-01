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
  async getRiskLimits(): Promise<{ limits: RiskConfig }> {
    return this.request('/risk/limits');
  }

  async updateRiskLimits(
    config: RiskConfig
  ): Promise<{ status: string; limits: RiskConfig }> {
    return this.request('/risk/limits', {
      method: 'PUT',
      body: JSON.stringify(config),
    });
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
