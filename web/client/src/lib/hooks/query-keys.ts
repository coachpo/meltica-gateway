type KeyPart = string | number | boolean | undefined;

type Serializable = Record<string, unknown>;

function serializeParams(params?: Serializable): string {
  if (!params) {
    return 'default';
  }
  const entries = Object.entries(params).filter(([, value]) => value !== undefined);
  if (entries.length === 0) {
    return 'default';
  }
  const normalised = entries
    .map(([key, value]) => [key, value] as const)
    .sort(([a], [b]) => a.localeCompare(b));
  return JSON.stringify(normalised);
}

export const queryKeys = {
  strategies(): [string] {
    return ['strategies'];
  },
  strategy(name: string): [string, KeyPart] {
    return ['strategy', name];
  },
  strategyModules(filters?: Serializable): [string, string] {
    return ['strategy-modules', serializeParams(filters)];
  },
  strategyModule(identifier: string): [string, KeyPart] {
    return ['strategy-module', identifier];
  },
  strategyModuleSource(identifier: string): [string, KeyPart] {
    return ['strategy-module-source', identifier];
  },
  strategyModuleUsage(selector: string, filters?: Serializable): [string, KeyPart, string] {
    return ['strategy-module-usage', selector, serializeParams(filters)];
  },
  providers(): [string] {
    return ['providers'];
  },
  provider(name: string): [string, KeyPart] {
    return ['provider', name];
  },
  adapters(): [string] {
    return ['adapters'];
  },
  instances(): [string] {
    return ['instances'];
  },
  instance(id: string): [string, KeyPart] {
    return ['instance', id];
  },
  instanceOrders(id: string, filters?: Serializable): [string, KeyPart, string] {
    return ['instance-orders', id, serializeParams(filters)];
  },
  instanceExecutions(id: string, filters?: Serializable): [string, KeyPart, string] {
    return ['instance-executions', id, serializeParams(filters)];
  },
  providerBalances(name: string, filters?: Serializable): [string, KeyPart, string] {
    return ['provider-balances', name, serializeParams(filters)];
  },
  riskLimits(): [string] {
    return ['risk-limits'];
  },
  runtimeConfig(): [string] {
    return ['runtime-config'];
  },
  configBackup(): [string] {
    return ['config-backup'];
  },
  contextBackup(): [string] {
    return ['context-backup'];
  },
  outbox(filters?: Serializable): [string, string] {
    return ['outbox', serializeParams(filters)];
  },
};
