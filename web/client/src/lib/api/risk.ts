import { z } from 'zod';
import type { RiskConfig } from '@/lib/types';
import { requestJson } from './http';
import { normalizeRiskLimitsResponse, serializeRiskLimitsPayload, type PartialRiskConfigResponse } from './normalizers';

const riskPayloadSchema = z.unknown();

export interface RiskLimitsResult {
  status?: string;
  limits: PartialRiskConfigResponse;
}

export async function fetchRiskLimits(): Promise<RiskLimitsResult> {
  const payload = await requestJson({
    path: '/risk/limits',
    schema: riskPayloadSchema,
  });

  const record = payload && typeof payload === 'object' ? (payload as Record<string, unknown>) : {};
  const rawLimits = Object.prototype.hasOwnProperty.call(record, 'limits') ? record.limits : payload;
  const status =
    typeof record.status === 'string' && record.status.trim().length > 0 ? record.status : undefined;

  const limits = normalizeRiskLimitsResponse(rawLimits);
  return status ? { status, limits } : { limits };
}

export async function updateRiskLimits(config: RiskConfig): Promise<RiskLimitsResult> {
  const payload = await requestJson({
    path: '/risk/limits',
    method: 'PUT',
    body: serializeRiskLimitsPayload(config),
    schema: riskPayloadSchema,
  });

  const record = payload && typeof payload === 'object' ? (payload as Record<string, unknown>) : {};
  const rawLimits = Object.prototype.hasOwnProperty.call(record, 'limits') ? record.limits : payload;
  const status =
    typeof record.status === 'string' && record.status.trim().length > 0 ? record.status : undefined;

  const limits = normalizeRiskLimitsResponse(rawLimits);
  return status ? { status, limits } : { limits };
}
