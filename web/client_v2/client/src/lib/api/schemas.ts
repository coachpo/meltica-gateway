import { z } from 'zod';

const strategyConfigSchema = z.object({
  name: z.string(),
  type: z.string(),
  description: z.string(),
  default: z.unknown().optional(),
  required: z.boolean(),
});

export const strategySchema = z.object({
  name: z.string(),
  displayName: z.string(),
  description: z.string(),
  tag: z.string().optional(),
  config: z.array(strategyConfigSchema),
  events: z.array(z.string()),
}).passthrough();

export const strategyListSchema = z.object({
  strategies: z.array(strategySchema),
});

const strategyModuleRevisionSchema = z.object({
  hash: z.string(),
  tag: z.string().optional(),
  path: z.string(),
  size: z.number(),
  retired: z.boolean().optional(),
}).passthrough();

const moduleRunningSummarySchema = z.object({
  hash: z.string(),
  instances: z
    .array(z.string())
    .nullish()
    .default([]),
  count: z.number(),
  firstSeen: z.string().nullable().optional(),
  lastSeen: z.string().nullable().optional(),
});

const strategyModuleResolutionSchema = z.object({
  name: z.string(),
  hash: z.string(),
  tag: z.string().optional(),
  file: z.string().optional(),
  path: z.string().optional(),
}).passthrough();

export const strategyModuleSummarySchema = z.object({
  name: z.string(),
  file: z.string(),
  path: z.string(),
  hash: z.string(),
  tag: z.string().optional(),
  tags: z.array(z.string()),
  tagAliases: z.record(z.string(), z.string()).optional(),
  revisions: z.array(strategyModuleRevisionSchema).optional(),
  running: z
    .array(moduleRunningSummarySchema)
    .nullish()
    .default([]),
  size: z.number(),
  metadata: strategySchema,
}).passthrough();

export const strategyModulesResponseSchema = z.object({
  modules: z.array(strategyModuleSummarySchema),
  total: z.number().optional(),
  offset: z.number().optional(),
  limit: z.number().nullable().optional(),
  strategyDirectory: z.string().optional(),
});

export const strategyModuleOperationResponseSchema = z.object({
  filename: z.string().optional(),
  status: z.string(),
  strategyDirectory: z.string(),
  module: strategyModuleResolutionSchema.nullable().optional(),
});

const lambdaStrategySpecSchema = z.object({
  identifier: z.string(),
  config: z.record(z.string(), z.unknown()),
  selector: z.string().optional(),
  tag: z.string().optional(),
  hash: z.string().optional(),
}).passthrough();

const providerSymbolsSchema = z.object({
  symbols: z.array(z.string()),
});

export const instanceSummarySchema = z.object({
  id: z.string(),
  strategyIdentifier: z.string(),
  strategyTag: z.string().optional(),
  strategyHash: z.string().optional(),
  strategySelector: z.string().optional(),
  providers: z.array(z.string()),
  aggregatedSymbols: z.array(z.string()),
  running: z.boolean(),
  baseline: z.boolean().optional(),
  dynamic: z.boolean().optional(),
  createdAt: z.string().nullable().optional(),
  updatedAt: z.string().nullable().optional(),
  metadata: z.record(z.string(), z.unknown()).nullable().optional(),
  usage: z
    .object({
      strategy: z.string(),
      hash: z.string(),
      instances: z
        .array(z.string())
        .nullish()
        .default([]),
      count: z.number(),
      firstSeen: z.string().nullable().optional(),
      lastSeen: z.string().nullable().optional(),
      running: z.boolean().optional(),
    })
    .optional(),
  links: z
    .object({
      self: z.string().optional(),
      usage: z.string().optional(),
    })
    .optional(),
}).passthrough();

export const instanceSpecSchema = z.object({
  id: z.string(),
  strategy: lambdaStrategySpecSchema,
  scope: z.record(z.string(), providerSymbolsSchema),
  providers: z.array(z.string()).optional(),
  aggregatedSymbols: z.array(z.string()).optional(),
  running: z.boolean().optional(),
  baseline: z.boolean().optional(),
  dynamic: z.boolean().optional(),
  metadata: z.record(z.string(), z.unknown()).nullable().optional(),
});

export const instanceActionResponseSchema = z.object({
  id: z.string(),
  status: z.string(),
  action: z.enum(['start', 'stop']),
});

export type InstanceActionResponse = z.infer<typeof instanceActionResponseSchema>;

const moduleRevisionUsageSchema = z.object({
  strategy: z.string(),
  hash: z.string(),
  instances: z
    .array(z.string())
    .nullish()
    .default([]),
  count: z.number(),
  firstSeen: z.string().nullable().optional(),
  lastSeen: z.string().nullable().optional(),
  running: z.boolean().optional(),
});

export const strategyModuleUsageResponseSchema = z.object({
  selector: z.string(),
  strategy: z.string(),
  hash: z.string(),
  usage: moduleRevisionUsageSchema,
  instances: z.array(instanceSummarySchema),
  total: z.number(),
  offset: z.number(),
  limit: z.number().nullable().optional(),
});

export const strategyRegistryExportSchema = z.object({
  registry: z.record(
    z.string(),
    z.object({
      tags: z.record(z.string(), z.string()),
      hashes: z.record(
        z.string(),
        z.object({
          tag: z.string(),
          path: z.string(),
        }),
      ),
    }),
  ),
  usage: z.array(moduleRevisionUsageSchema),
});

const instrumentSchema = z.object({
  symbol: z.string(),
  type: z.string().optional(),
  baseAsset: z.string().nullable().optional(),
  baseCurrency: z.string().nullable().optional(),
  quoteAsset: z.string().nullable().optional(),
  quoteCurrency: z.string().nullable().optional(),
  venue: z.string().nullable().optional(),
  expiry: z.string().nullable().optional(),
  contractValue: z.number().nullable().optional(),
});

const settingsSchema = z.object({
  name: z.string(),
  type: z.string(),
  default: z.unknown().optional(),
  required: z.boolean(),
});

export const adapterMetadataSchema = z.object({
  identifier: z.string(),
  displayName: z.string(),
  venue: z.string(),
  description: z.string().optional(),
  capabilities: z.array(z.string()),
  settingsSchema: z.array(settingsSchema),
});

export const providerStatusSchema = z.enum(['pending', 'starting', 'running', 'stopped', 'failed']);

export const providerSchema = z.object({
  name: z.string(),
  adapter: z.string(),
  identifier: z.string(),
  instrumentCount: z.number(),
  settings: z.record(z.string(), z.unknown()),
  running: z.boolean(),
  status: providerStatusSchema,
  startupError: z.string().optional(),
  dependentInstances: z
    .array(z.string())
    .nullish()
    .default([]),
  dependentInstanceCount: z
    .number()
    .nullish()
    .default(0),
});

export const providerDetailSchema = providerSchema.extend({
  instruments: z.array(instrumentSchema).nullish().default([]),
  adapter: adapterMetadataSchema,
});

export const providersResponseSchema = z.object({
  providers: z.array(providerSchema),
});

export const adaptersResponseSchema = z.object({
  adapters: z.array(adapterMetadataSchema),
});

export const instancesResponseSchema = z.object({
  instances: z.array(instanceSummarySchema),
});

export const instanceHistoryParamsSchema = z.object({
  limit: z.number().optional(),
  provider: z.string().optional(),
  states: z.array(z.string()).optional(),
});

const orderRecordSchema = z.object({
  id: z.string(),
  provider: z.string(),
  strategyInstance: z.string(),
  clientOrderId: z.string(),
  symbol: z.string(),
  side: z.string(),
  type: z.string(),
  quantity: z.string(),
  price: z.string().nullable().optional(),
  state: z.string(),
  externalReference: z.string().nullable().optional(),
  placedAt: z.number(),
  metadata: z.record(z.string(), z.unknown()).optional(),
  acknowledgedAt: z.number().nullable().optional(),
  completedAt: z.number().nullable().optional(),
  createdAt: z.number(),
  updatedAt: z.number(),
});

const executionRecordSchema = z.object({
  orderId: z.string(),
  provider: z.string(),
  strategyInstance: z.string(),
  executionId: z.string(),
  quantity: z.string(),
  price: z.string(),
  fee: z.string().nullable().optional(),
  feeAsset: z.string().nullable().optional(),
  liquidity: z.string().nullable().optional(),
  tradedAt: z.number(),
  metadata: z.record(z.string(), z.unknown()).optional(),
  createdAt: z.number(),
});

const balanceRecordSchema = z.object({
  provider: z.string(),
  asset: z.string(),
  total: z.string(),
  available: z.string(),
  snapshotAt: z.number(),
  metadata: z.record(z.string(), z.unknown()).optional(),
  createdAt: z.number(),
  updatedAt: z.number(),
});

export const orderHistoryResponseSchema = z.object({
  orders: z.array(orderRecordSchema),
  count: z.number(),
});

export const executionHistoryResponseSchema = z.object({
  executions: z.array(executionRecordSchema),
  count: z.number(),
});

export const balanceHistoryResponseSchema = z.object({
  balances: z.array(balanceRecordSchema),
  count: z.number(),
});

export const runtimeConfigSnapshotSchema = z.object({
  config: z.record(z.string(), z.unknown()),
  source: z.string(),
  persistedAt: z.string().nullable().optional(),
  filePath: z.string().nullable().optional(),
  metadata: z.record(z.string(), z.unknown()).nullable().optional(),
});

export const configBackupSchema = z.object({
  version: z.string(),
  generatedAt: z.string(),
  environment: z.string(),
  meta: z
    .object({
      name: z.string().optional(),
      version: z.string().optional(),
      description: z.string().optional(),
    })
    .optional(),
  runtime: z.record(z.string(), z.unknown()),
  providers: z.object({
    config: z.record(z.string(), z.record(z.string(), z.unknown())).nullable(),
    runtime: z.array(
      z.object({
        name: z.string(),
        adapter: z.string(),
        identifier: z.string(),
        instrumentCount: z.number(),
        settings: z.record(z.string(), z.unknown()).nullable().optional(),
        running: z.boolean(),
        status: providerStatusSchema,
        startupError: z.string().optional(),
      }),
    ),
  }),
  lambdas: z.object({
    instances: z.array(
      z.object({
        id: z.string(),
        strategy: lambdaStrategySpecSchema,
        providers: z.array(z.string()).optional(),
        providerSymbols: z.record(z.string(), providerSymbolsSchema).optional(),
        aggregatedSymbols: z.array(z.string()).optional(),
        running: z.boolean().optional(),
        baseline: z.boolean().optional(),
        dynamic: z.boolean().optional(),
        createdAt: z.string().nullable().optional(),
        updatedAt: z.string().nullable().optional(),
        metadata: z.record(z.string(), z.unknown()).nullable().optional(),
      }),
    ),
  }),
});

export const restoreConfigResponseSchema = z.object({
  status: z.string(),
  providers: z.number(),
  lambdas: z.number(),
});

export const contextBackupSchema = z.object({
  providers: z.array(z.record(z.string(), z.unknown())),
  lambdas: z.array(instanceSpecSchema),
  risk: z.record(z.string(), z.unknown()),
});

export const restoreContextResponseSchema = z.object({
  status: z.string(),
});

export const strategyRefreshResponseSchema = z.object({
  status: z.string(),
  results: z
    .array(
      z.object({
        selector: z.string(),
        strategy: z.string().optional(),
        hash: z.string().optional(),
        previousHash: z.string().optional(),
        instances: z.array(z.string()).optional(),
        reason: z.string().optional(),
      }),
    )
    .optional(),
});

export const outboxEventSchema = z.object({
  id: z.number(),
  aggregateType: z.string(),
  aggregateID: z.string(),
  eventType: z.string(),
  payload: z.record(z.string(), z.unknown()),
  headers: z.record(z.string(), z.unknown()),
  availableAt: z.string(),
  publishedAt: z.string().nullable().optional(),
  attempts: z.number(),
  lastError: z.string().nullable().optional(),
  delivered: z.boolean(),
  createdAt: z.string(),
});

export const outboxListResponseSchema = z.object({
  events: z.array(outboxEventSchema),
  count: z.number(),
});

export const outboxDeleteResponseSchema = z.object({
  id: z.number(),
  status: z.string(),
});
