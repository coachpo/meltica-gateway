import { z } from 'zod';
import type { ConfigBackup, RestoreConfigResponse, RuntimeConfig, RuntimeConfigSnapshot } from '@/lib/types';
import { requestJson } from './http';
import { configBackupSchema, restoreConfigResponseSchema } from './schemas';
import { normalizeRuntimeConfigSnapshot } from './normalizers';

const runtimePayloadSchema = z.unknown();

async function resolveRuntimeSnapshot(payload: unknown): Promise<RuntimeConfigSnapshot> {
  return normalizeRuntimeConfigSnapshot(payload);
}

export async function fetchRuntimeConfig(): Promise<RuntimeConfigSnapshot> {
  const payload = await requestJson({
    path: '/config/runtime',
    schema: runtimePayloadSchema,
  });
  return resolveRuntimeSnapshot(payload);
}

export async function updateRuntimeConfig(payload: RuntimeConfig): Promise<RuntimeConfigSnapshot> {
  const response = await requestJson({
    path: '/config/runtime',
    method: 'PUT',
    body: payload,
    schema: runtimePayloadSchema,
  });
  if (response === undefined) {
    return fetchRuntimeConfig();
  }
  return resolveRuntimeSnapshot(response);
}

export async function revertRuntimeConfig(): Promise<RuntimeConfigSnapshot> {
  const response = await requestJson({
    path: '/config/runtime',
    method: 'DELETE',
    schema: runtimePayloadSchema,
  });
  if (response === undefined) {
    return fetchRuntimeConfig();
  }
  return resolveRuntimeSnapshot(response);
}

export async function fetchConfigBackup(): Promise<ConfigBackup> {
  return requestJson({
    path: '/config/backup',
    schema: configBackupSchema,
  });
}

export async function restoreConfigBackup(payload: ConfigBackup): Promise<RestoreConfigResponse> {
  return requestJson({
    path: '/config/backup',
    method: 'POST',
    body: payload,
    schema: restoreConfigResponseSchema,
  });
}
