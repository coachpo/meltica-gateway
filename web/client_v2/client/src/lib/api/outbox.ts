import type { OutboxDeleteResponse, OutboxListResponse, OutboxQuery } from '@/lib/types';
import { outboxDeleteResponseSchema, outboxListResponseSchema } from './schemas';
import { requestJson } from './http';

export async function fetchOutboxEvents(params?: OutboxQuery): Promise<OutboxListResponse> {
  return requestJson({
    path: '/outbox',
    searchParams: params,
    schema: outboxListResponseSchema,
  });
}

export async function deleteOutboxEvent(id: number): Promise<OutboxDeleteResponse> {
  return requestJson({
    path: `/outbox/${encodeURIComponent(String(id))}`,
    method: 'DELETE',
    schema: outboxDeleteResponseSchema,
  });
}
