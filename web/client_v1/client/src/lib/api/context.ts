import type { ContextBackupPayload, RestoreContextResponse } from '@/lib/types';
import { contextBackupSchema, restoreContextResponseSchema } from './schemas';
import { requestJson } from './http';

export async function fetchContextBackup(): Promise<ContextBackupPayload> {
  return requestJson({
    path: '/context/backup',
    schema: contextBackupSchema,
  });
}

export async function restoreContextBackup(payload: ContextBackupPayload): Promise<RestoreContextResponse> {
  return requestJson({
    path: '/context/backup',
    method: 'POST',
    body: payload,
    schema: restoreContextResponseSchema,
  });
}
