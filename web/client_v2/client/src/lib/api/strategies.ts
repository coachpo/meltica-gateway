import type {
  Strategy,
  StrategyModuleOperationResponse,
  StrategyModulePayload,
  StrategyModuleSummary,
  StrategyModuleUsageResponse,
  StrategyModulesResponse,
  StrategyRefreshRequest,
  StrategyRefreshResponse,
  StrategyRegistryExport,
} from '@/lib/types';
import {
  strategyListSchema,
  strategyModuleOperationResponseSchema,
  strategyModuleSummarySchema,
  strategyModuleUsageResponseSchema,
  strategyModulesResponseSchema,
  strategyRegistryExportSchema,
  strategySchema,
  strategyRefreshResponseSchema,
} from './schemas';
import { requestJson, requestText } from './http';

export interface StrategyModulesFilters {
  strategy?: string;
  hash?: string;
  runningOnly?: boolean;
  limit?: number;
  offset?: number;
}

export interface StrategyModuleUsageFilters {
  limit?: number;
  offset?: number;
  includeStopped?: boolean;
}

export async function fetchStrategies(): Promise<Strategy[]> {
  const data = await requestJson({
    path: '/strategies',
    schema: strategyListSchema,
  });
  return data.strategies;
}

export async function fetchStrategy(name: string): Promise<Strategy> {
  return requestJson({
    path: `/strategies/${encodeURIComponent(name)}`,
    schema: strategySchema,
  });
}

export async function fetchStrategyModules(filters?: StrategyModulesFilters): Promise<StrategyModulesResponse> {
  return requestJson({
    path: '/strategies/modules',
    searchParams: filters,
    schema: strategyModulesResponseSchema,
  });
}

export async function fetchStrategyModule(identifier: string): Promise<StrategyModuleSummary> {
  return requestJson({
    path: `/strategies/modules/${encodeURIComponent(identifier)}`,
    schema: strategyModuleSummarySchema,
  });
}

export async function fetchStrategyModuleUsage(
  selector: string,
  filters?: StrategyModuleUsageFilters,
): Promise<StrategyModuleUsageResponse> {
  return requestJson({
    path: `/strategies/modules/${encodeURIComponent(selector)}/usage`,
    searchParams: filters,
    schema: strategyModuleUsageResponseSchema,
  });
}

export async function fetchStrategyModuleSource(identifier: string): Promise<string> {
  return requestText({
    path: `/strategies/modules/${encodeURIComponent(identifier)}/source`,
  });
}

export async function createStrategyModule(
  payload: StrategyModulePayload,
): Promise<StrategyModuleOperationResponse> {
  return requestJson({
    path: '/strategies/modules',
    method: 'POST',
    body: payload,
    schema: strategyModuleOperationResponseSchema,
  });
}

export async function updateStrategyModule(
  identifier: string,
  payload: StrategyModulePayload,
): Promise<StrategyModuleOperationResponse> {
  return requestJson({
    path: `/strategies/modules/${encodeURIComponent(identifier)}`,
    method: 'PUT',
    body: payload,
    schema: strategyModuleOperationResponseSchema,
  });
}

export async function deleteStrategyModule(identifier: string): Promise<void> {
  await requestJson({
    path: `/strategies/modules/${encodeURIComponent(identifier)}`,
    method: 'DELETE',
  });
}

export async function refreshStrategyCatalog(
  payload?: StrategyRefreshRequest,
): Promise<StrategyRefreshResponse> {
  return requestJson({
    path: '/strategies/refresh',
    method: 'POST',
    body: payload,
    schema: strategyRefreshResponseSchema,
  });
}

export async function exportStrategyRegistry(): Promise<StrategyRegistryExport> {
  return requestJson({
    path: '/strategies/registry',
    schema: strategyRegistryExportSchema,
  });
}
