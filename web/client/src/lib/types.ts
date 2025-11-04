// API Types based on docs/lambdas-api.md

export interface StrategyConfig {
  name: string;
  type: string;
  description: string;
  default?: unknown;
  required: boolean;
}

export interface Strategy {
  name: string;
  displayName: string;
  description: string;
  version?: string;
  config: StrategyConfig[];
  events: string[];
}

export interface StrategyModuleRevision {
  hash: string;
  tag?: string;
  path: string;
  version?: string;
  size: number;
  retired?: boolean;
}

export interface ModuleRunningSummary {
  hash: string;
  instances: string[];
  count: number;
  firstSeen?: string | null;
  lastSeen?: string | null;
}

export interface StrategyModuleSummary {
  name: string;
  file: string;
  path: string;
  hash: string;
  version?: string;
  tags: string[];
  tagAliases?: Record<string, string>;
  revisions?: StrategyModuleRevision[];
  running?: ModuleRunningSummary[];
  size: number;
  metadata: Strategy;
}

export interface StrategyModulesResponse {
  modules: StrategyModuleSummary[];
  total?: number;
  offset?: number;
  limit?: number | null;
  strategyDirectory?: string;
}

export interface StrategyModulePayload {
  filename?: string;
  source: string;
  name?: string;
  tag?: string;
  aliases?: string[];
  promoteLatest?: boolean;
}

export interface StrategyModuleResolution {
  name: string;
  hash: string;
  tag?: string;
  version?: string;
  file?: string;
  path?: string;
}

export interface StrategyModuleOperationResponse {
  filename?: string;
  status: string;
  strategyDirectory: string;
  module?: StrategyModuleResolution | null;
}

export type ProviderSettings = Record<string, unknown>;

export type ProviderStatus = 'pending' | 'starting' | 'running' | 'stopped' | 'failed';

export interface Provider {
  name: string;
  adapter: string;
  identifier: string;
  instrumentCount: number;
  settings: ProviderSettings;
  running: boolean;
  status: ProviderStatus;
  startupError?: string;
  dependentInstances: string[];
  dependentInstanceCount: number;
}

export interface Instrument {
  symbol: string;
  type?: string;
  baseAsset?: string | null;
  baseCurrency?: string | null;
  quoteAsset?: string | null;
  quoteCurrency?: string | null;
  venue?: string | null;
  expiry?: string | null;
  contractValue?: number | null;
  contractCurrency?: string | null;
  strike?: number | null;
  optionType?: string | null;
  priceIncrement?: number | string | null;
  quantityIncrement?: number | string | null;
  pricePrecision?: number | null;
  quantityPrecision?: number | null;
  notionalPrecision?: number | null;
  minNotional?: number | string | null;
  minQuantity?: number | string | null;
  maxQuantity?: number | string | null;
  [key: string]: unknown;
}

export interface SettingsSchema {
  name: string;
  type: string;
  default?: unknown;
  required: boolean;
}

export interface AdapterMetadata {
  identifier: string;
  displayName: string;
  venue: string;
  description?: string;
  capabilities: string[];
  settingsSchema: SettingsSchema[];
}

export interface ProviderDetail extends Provider {
  instruments: Instrument[];
  adapter: AdapterMetadata;
}

export interface ProviderRequest {
  name: string;
  adapter: {
    identifier: string;
    config: Record<string, unknown>;
  };
  enabled?: boolean;
}

export interface InstanceSummary {
  id: string;
  strategyIdentifier: string;
  strategyTag?: string;
  strategyHash?: string;
  strategyVersion?: string;
  strategySelector?: string;
  providers: string[];
  aggregatedSymbols: string[];
  running: boolean;
  usage?: ModuleRevisionUsage;
  links?: InstanceLinks;
}

export interface ProviderSymbols {
  symbols: string[];
}

export interface InstanceSpec {
  id: string;
  strategy: {
    identifier: string;
    selector?: string;
    tag?: string;
    hash?: string;
    version?: string;
    config: Record<string, unknown>;
  };
  scope: Record<string, ProviderSymbols>;
  providers?: string[];
  aggregatedSymbols?: string[];
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

export type FanoutWorkersSetting = number | 'auto' | 'default' | string;

export interface EventbusRuntimeConfig {
  bufferSize: number;
  fanoutWorkers: FanoutWorkersSetting;
}

export interface ObjectPoolRuntimeConfig {
  size: number;
  waitQueueSize: number;
}

export interface PoolRuntimeConfig {
  event: ObjectPoolRuntimeConfig;
  orderRequest: ObjectPoolRuntimeConfig;
}

export interface ApiServerConfig {
  addr: string;
}

export interface TelemetryConfig {
  otlpEndpoint: string;
  serviceName: string;
  otlpInsecure: boolean;
  enableMetrics: boolean;
}

export interface RuntimeConfig {
  eventbus: EventbusRuntimeConfig;
  pools: PoolRuntimeConfig;
  risk: RiskConfig;
  apiServer: ApiServerConfig;
  telemetry: TelemetryConfig;
}

export type RuntimeConfigSource = 'runtime' | 'file' | 'bootstrap';

export interface RuntimeConfigSnapshot {
  config: RuntimeConfig;
  source: RuntimeConfigSource;
  persistedAt?: string | null;
  filePath?: string | null;
  metadata?: Record<string, unknown> | null;
}

export interface ProviderRuntimeMetadata {
  name: string;
  adapter: string;
  identifier: string;
  instrumentCount: number;
  settings?: Record<string, unknown> | null;
  running: boolean;
  status: ProviderStatus;
  startupError?: string;
}

export interface LambdaStrategySpec {
  identifier: string;
  config: Record<string, unknown>;
}

export interface LambdaManifestEntry {
  id: string;
  strategy: LambdaStrategySpec;
  scope: Record<string, ProviderSymbols>;
}

export interface LambdaInstanceSnapshot {
  id: string;
  strategy: LambdaStrategySpec;
  providers: string[];
  providerSymbols: Record<string, ProviderSymbols>;
  aggregatedSymbols: string[];
  running: boolean;
}

export interface ConfigBackup {
  version: string;
  generatedAt: string;
  environment: string;
  meta: {
    name?: string;
    version?: string;
    description?: string;
  };
  runtime: RuntimeConfig;
  providers: {
    config: Record<string, Record<string, unknown>> | null;
    runtime: ProviderRuntimeMetadata[];
  };
  lambdas: {
    manifest: {
      lambdas: LambdaManifestEntry[];
    };
    instances: LambdaInstanceSnapshot[];
  };
}

export interface RestoreConfigResponse {
  status: string;
  providers: number;
  lambdas: number;
}

export interface ContextBackupPayload {
  providers: Record<string, unknown>[];
  lambdas: Record<string, unknown>[];
  risk: Record<string, unknown>;
}

export interface RestoreContextResponse {
  status: string;
}

export interface StrategyRefreshRequest {
  hashes?: string[];
  strategies?: string[];
}

export interface StrategyRefreshResult {
  selector: string;
  strategy?: string;
  hash?: string;
  previousHash?: string;
  instances?: string[];
  reason?: string;
}

export interface StrategyRefreshResponse {
  status: string;
  results?: StrategyRefreshResult[];
}

export interface ModuleRevisionUsage {
  strategy: string;
  hash: string;
  instances: string[];
  count: number;
  firstSeen?: string | null;
  lastSeen?: string | null;
  running?: boolean;
}

export interface InstanceLinks {
  self?: string;
  usage?: string;
}

export interface StrategyModuleUsageResponse {
  selector: string;
  strategy: string;
  hash: string;
  usage: ModuleRevisionUsage;
  instances: InstanceSummary[];
  total: number;
  offset: number;
  limit?: number | null;
}

export interface StrategyRegistryLocation {
  tag: string;
  path: string;
}

export interface StrategyRegistryEntry {
  tags: Record<string, string>;
  hashes: Record<string, StrategyRegistryLocation>;
}

export interface StrategyRegistryExport {
  registry: Record<string, StrategyRegistryEntry>;
  usage: ModuleRevisionUsage[];
}
