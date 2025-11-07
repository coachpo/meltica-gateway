import type {
  ExecutionHistoryResponse,
  InstanceSpec,
  InstanceSummary,
  OrderHistoryResponse,
} from '@/lib/types';
import {
  executionHistoryResponseSchema,
  instanceSpecSchema,
  instancesResponseSchema,
  orderHistoryResponseSchema,
} from './schemas';
import { requestJson } from './http';

export interface InstanceListFilters {
  running?: boolean;
}

export interface OrderHistoryFilters {
  limit?: number;
  provider?: string;
  states?: string[];
}

export interface ExecutionHistoryFilters {
  limit?: number;
  provider?: string;
  orderId?: string;
}

export async function fetchInstances(): Promise<InstanceSummary[]> {
  const data = await requestJson({
    path: '/strategy/instances',
    schema: instancesResponseSchema,
  });
  return data.instances;
}

export async function fetchInstance(id: string): Promise<InstanceSpec> {
  return requestJson({
    path: `/strategy/instances/${encodeURIComponent(id)}`,
    schema: instanceSpecSchema,
  });
}

export async function createInstance(spec: InstanceSpec): Promise<InstanceSpec> {
  return requestJson({
    path: '/strategy/instances',
    method: 'POST',
    body: spec,
    schema: instanceSpecSchema,
  });
}

export async function updateInstance(id: string, spec: InstanceSpec): Promise<InstanceSpec> {
  return requestJson({
    path: `/strategy/instances/${encodeURIComponent(id)}`,
    method: 'PUT',
    body: spec,
    schema: instanceSpecSchema,
  });
}

export async function deleteInstance(id: string): Promise<void> {
  await requestJson({
    path: `/strategy/instances/${encodeURIComponent(id)}`,
    method: 'DELETE',
  });
}

export async function startInstance(id: string): Promise<InstanceSpec> {
  return requestJson({
    path: `/strategy/instances/${encodeURIComponent(id)}/start`,
    method: 'POST',
    schema: instanceSpecSchema,
  });
}

export async function stopInstance(id: string): Promise<InstanceSpec> {
  return requestJson({
    path: `/strategy/instances/${encodeURIComponent(id)}/stop`,
    method: 'POST',
    schema: instanceSpecSchema,
  });
}

export async function fetchInstanceOrders(
  id: string,
  filters?: OrderHistoryFilters,
): Promise<OrderHistoryResponse> {
  return requestJson({
    path: `/strategy/instances/${encodeURIComponent(id)}/orders`,
    searchParams: filters,
    schema: orderHistoryResponseSchema,
  });
}

export async function fetchInstanceExecutions(
  id: string,
  filters?: ExecutionHistoryFilters,
): Promise<ExecutionHistoryResponse> {
  return requestJson({
    path: `/strategy/instances/${encodeURIComponent(id)}/executions`,
    searchParams: filters,
    schema: executionHistoryResponseSchema,
  });
}
