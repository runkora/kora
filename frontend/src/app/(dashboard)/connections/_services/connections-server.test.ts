import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@runkora/server/api-error';

const listConnections = vi.hoisted(() => vi.fn());
const agentsGet = vi.hoisted(() => vi.fn());

vi.mock('@runkora/server/client', async () => ({
  ApiError: (await import('@runkora/server/api-error')).ApiError,
}));
vi.mock('@runkora/server/connections-client', () => ({ listConnections }));
vi.mock('@runkora/server/agents-client', () => ({ agentsClient: () => ({ get: agentsGet }) }));

import { fetchConnections } from './connections-server';

describe('fetchConnections', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    agentsGet.mockResolvedValue({ agent_types: [] });
  });

  it('reports a refused catalog as no access', async () => {
    listConnections.mockRejectedValue(new ApiError(403, 'forbidden', 'forbidden'));
    expect(await fetchConnections('token', 't-1')).toEqual({
      ok: false,
      message: 'You do not have access to connections.',
    });
  });

  it('shows an empty catalog when the gateway is unreachable', async () => {
    listConnections.mockRejectedValue(new ApiError(502, 'UPSTREAM_ERROR', 'down'));
    expect(await fetchConnections('token', 't-1')).toEqual({
      ok: true,
      rows: [],
      usedByError: null,
    });
  });
});
