import { beforeEach, describe, expect, it, vi } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import type { AgentInsightsPeriod, AgentInsightsResponse } from '@orbit/shared/api';

const api = vi.hoisted(() => ({ fetchAgentInsights: vi.fn() }));
vi.mock('../_services/agents-insights-api', () => api);

import { useAgentsInsights } from './use-agents-insights';

const AGENTS = [
  { slug: 'coder', name: 'Coder', identityMode: 'autonomous' },
  { slug: 'notes', name: 'Notes', identityMode: 'delegated' },
];

function insights(period: AgentInsightsPeriod, cost: number): AgentInsightsResponse {
  return {
    period,
    usage: [
      {
        agent_slug: 'coder',
        prompt_tokens: 1,
        completion_tokens: 1,
        cache_read_tokens: 0,
        cache_creation_tokens: 0,
        total_tokens: 2,
        estimated_cost_usd: cost,
      },
    ],
    total_cost_usd: cost,
    previous_total_usd: 0,
    models: [],
    trend: [{ start: '2026-09-27T00:00:00Z', cost_usd: cost }],
    all_delegated_usage: false,
    budgets: [
      {
        agent_slug: 'coder',
        windows: [
          { cadence: 'monthly', limit_usd: 10, spend_usd: 2, resets_at: '2026-11-01T00:00:00Z' },
        ],
      },
      {
        agent_slug: 'notes',
        windows: [
          { cadence: 'monthly', limit_usd: 10, spend_usd: 9, resets_at: '2026-11-01T00:00:00Z' },
        ],
      },
    ],
    tenant_cap: {
      limit_usd: 100,
      reset_cadence: 'monthly',
      spend_usd: 20,
      resets_at: '2026-11-01T00:00:00Z',
    },
  };
}

type Period = '7d' | '24h';

beforeEach(() => {
  vi.clearAllMocks();
  api.fetchAgentInsights.mockImplementation((period: string) =>
    Promise.resolve(insights(period as AgentInsightsPeriod, period === '7d' ? 7 : 24)),
  );
});

describe('useAgentsInsights', () => {
  it('starts loading, then reports the period, the budgets of limited non-delegated agents and the cap', async () => {
    const { result } = renderHook(() => useAgentsInsights('7d', AGENTS, true));
    expect(result.current.status).toBe('loading');
    expect(result.current.insights).toBeUndefined();

    await waitFor(() => expect(result.current.status).toBe('ready'));
    expect(result.current.insights?.byPeriod?.['7d']?.usage.coder?.estimated_cost_usd).toBe(7);
    expect(result.current.insights?.budgets?.map((b) => b.name)).toEqual(['Coder']);
    expect(result.current.insights?.tenantBudget?.limit).toBe(100);
  });

  it('is ready at once for the period the server render supplied, and does not fetch it', () => {
    const { result } = renderHook(() => useAgentsInsights('7d', AGENTS, true, insights('7d', 7)));
    expect(result.current.status).toBe('ready');
    expect(result.current.insights?.byPeriod?.['7d']?.total).toBe(7);
    expect(result.current.insights?.budgets).toHaveLength(1);
    expect(api.fetchAgentInsights).not.toHaveBeenCalled();
  });

  it('fetches another period once, when first selected, and keeps both', async () => {
    const { result, rerender } = renderHook(
      ({ period }: { period: Period }) =>
        useAgentsInsights(period, AGENTS, true, insights('7d', 7)),
      { initialProps: { period: '7d' } },
    );

    rerender({ period: '24h' });
    expect(result.current.status).toBe('loading');
    await waitFor(() => expect(result.current.status).toBe('ready'));
    expect(api.fetchAgentInsights).toHaveBeenCalledTimes(1);

    rerender({ period: '7d' });
    rerender({ period: '24h' });
    expect(api.fetchAgentInsights).toHaveBeenCalledTimes(1);
    expect(Object.keys(result.current.insights?.byPeriod ?? {}).sort()).toEqual(['24h', '7d']);
  });

  it('leaves a failed part out and retries the period when it is selected again', async () => {
    api.fetchAgentInsights.mockResolvedValueOnce(null);
    const { result, rerender } = renderHook(
      ({ period }: { period: Period }) => useAgentsInsights(period, AGENTS, true),
      { initialProps: { period: '7d' } },
    );
    await waitFor(() => expect(result.current.status).toBe('failed'));
    expect(result.current.insights).toBeUndefined();

    rerender({ period: '24h' });
    await waitFor(() => expect(result.current.status).toBe('ready'));
    rerender({ period: '7d' });
    await waitFor(() => expect(result.current.status).toBe('ready'));
  });
});
