import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchAgentOverview } from './agent-detail-api';

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('agent overview fetcher', () => {
  it('requests the overview by slug and period', async () => {
    const fetchMock = vi.fn().mockResolvedValue(Response.json({ period: '30d' }));
    vi.stubGlobal('fetch', fetchMock);

    const { data, error } = await fetchAgentOverview('token-probe', '30d');

    expect(error).toBeNull();
    expect(data?.period).toBe('30d');
    expect(fetchMock).toHaveBeenCalledWith('/api/agents/types/overview/token-probe?period=30d', {
      cache: 'no-store',
    });
  });

  it('reports a failed request as no data', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 503 })));
    expect(await fetchAgentOverview('token-probe', '7d')).toEqual({
      data: null,
      error: 'Request failed (503)',
    });
  });
});
