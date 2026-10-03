import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchAgentTypeUsage } from '../[slug]/_services/agent-detail-api';

function usage(slug: string, total: number) {
  return {
    agent_slug: slug,
    prompt_tokens: total,
    completion_tokens: 0,
    cache_read_tokens: 0,
    cache_creation_tokens: 0,
    total_tokens: total,
    estimated_cost_usd: total / 1000,
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('agent usage fetcher', () => {
  it('requests the overview usage by slug', async () => {
    const fetchMock = vi.fn().mockResolvedValue(Response.json(usage('token-probe', 150)));
    vi.stubGlobal('fetch', fetchMock);

    const { usage: got, error } = await fetchAgentTypeUsage('token-probe');

    expect(error).toBeNull();
    expect(got?.total_tokens).toBe(150);
    expect(fetchMock).toHaveBeenCalledWith('/api/agents/types/usage/token-probe?period=7d', {
      cache: 'no-store',
    });
  });
});
