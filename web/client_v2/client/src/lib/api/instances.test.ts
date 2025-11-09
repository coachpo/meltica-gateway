import { describe, expect, it, vi, beforeEach } from 'vitest';

import { instanceActionResponseSchema } from '@/lib/api/schemas';
import { startInstance, stopInstance } from '@/lib/api/instances';

vi.mock('@/lib/api/http', () => ({
  requestJson: vi.fn(),
}));

const { requestJson } = await import('@/lib/api/http');
const mockRequestJson = requestJson as ReturnType<typeof vi.fn>;

beforeEach(() => {
  mockRequestJson.mockReset();
});

describe('instance action helpers', () => {
  it('startInstance posts to the start endpoint and expects ack schema', async () => {
    mockRequestJson.mockResolvedValue({ id: 'foo', status: 'ok', action: 'start' });

    const response = await startInstance('foo');

    expect(mockRequestJson).toHaveBeenCalledWith({
      path: '/strategy/instances/foo/start',
      method: 'POST',
      schema: instanceActionResponseSchema,
    });
    expect(response).toEqual({ id: 'foo', status: 'ok', action: 'start' });
  });

  it('stopInstance posts to the stop endpoint and expects ack schema', async () => {
    mockRequestJson.mockResolvedValue({ id: 'bar', status: 'ok', action: 'stop' });

    const response = await stopInstance('bar');

    expect(mockRequestJson).toHaveBeenCalledWith({
      path: '/strategy/instances/bar/stop',
      method: 'POST',
      schema: instanceActionResponseSchema,
    });
    expect(response).toEqual({ id: 'bar', status: 'ok', action: 'stop' });
  });
});
