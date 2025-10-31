import {
  Strategy,
  Provider,
  ProviderDetail,
  AdapterMetadata,
  InstanceSummary,
  InstanceSpec,
  RiskConfig,
  ApiError,
} from './types';

const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8880';

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

    if (!response.ok) {
      const error: ApiError = await response.json();
      throw new Error(error.error || 'Request failed');
    }

    return response.json();
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
}

export const apiClient = new ApiClient();
