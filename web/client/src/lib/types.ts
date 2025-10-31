// API Types based on docs/lambdas-api.md

export interface StrategyConfig {
  name: string;
  type: string;
  description: string;
  default?: any;
  required: boolean;
}

export interface Strategy {
  name: string;
  displayName: string;
  description: string;
  config: StrategyConfig[];
  events: string[];
}

export interface ProviderSettings {
  [key: string]: any;
}

export interface Provider {
  name: string;
  exchange: string;
  identifier: string;
  instrumentCount: number;
  settings: ProviderSettings;
}

export interface Instrument {
  symbol: string;
  baseAsset: string;
  quoteAsset: string;
  pricePrecision: number;
  quantityPrecision: number;
}

export interface SettingsSchema {
  name: string;
  type: string;
  default?: any;
  required: boolean;
}

export interface AdapterMetadata {
  identifier: string;
  displayName: string;
  venue: string;
  capabilities: string[];
  settingsSchema: SettingsSchema[];
}

export interface ProviderDetail extends Provider {
  instruments: Instrument[];
  adapter: AdapterMetadata;
}

export interface InstanceSummary {
  id: string;
  strategyIdentifier: string;
  providers: string[];
  aggregatedSymbols: string[];
  autoStart: boolean;
  running: boolean;
}

export interface ProviderSymbols {
  symbols: string[];
}

export interface InstanceSpec {
  id: string;
  strategy: {
    identifier: string;
    config: Record<string, any>;
  };
  scope: Record<string, ProviderSymbols>;
  providers?: string[];
  aggregatedSymbols?: string[];
  autoStart?: boolean;
  running?: boolean;
}

export interface CircuitBreakerConfig {
  enabled: boolean;
  threshold: number;
  cooldown: string;
}

export interface RiskConfig {
  maxPositionSize: string;
  maxNotionalValue: string;
  notionalCurrency: string;
  orderThrottle: number;
  orderBurst: number;
  maxConcurrentOrders: number;
  priceBandPercent: number;
  allowedOrderTypes: string[];
  killSwitchEnabled: boolean;
  maxRiskBreaches: number;
  circuitBreaker: CircuitBreakerConfig;
}

export interface ApiError {
  status: string;
  error: string;
}
