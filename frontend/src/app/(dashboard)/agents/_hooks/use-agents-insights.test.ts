import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import type {
  AgentBudgetsResponse,
  AgentInsightsPeriod,
  AgentInsightsResponse,
} from '@orbit/shared/api';

const api = vi.hoisted(() => ({
  fetchAgentInsights: vi.fn(),
  fetchAgentBudgets: vi.fn(),
}));
vi.mock('../_services/agents-insights-api', () => api);

import { useAgentsInsights } from './use-agents-insights';

const AGENTS = [{ slug: 'coder', name: 'Coder' }];

function insights(period: AgentInsightsPeriod, cost: number): AgentInsightsResponse {
  return {
    period,
    start: '2026-09-27T00:00:00Z',
    end: '2026-10-04T00:00:00Z',
    bucket_seconds: 86400,
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
  };
}

const BUDGETS: AgentBudgetsResponse = {
  agents: [
    {
      agent_slug: 'coder',
      week_to_date_usd: 1,
      month_to_date_usd: 2,
      limit_usd: 10,
      reset_cadence: 'monthly',
    },
  ],
  tenant_cap: { limit_usd: 100, reset_cadence: 'monthly', spend_usd: 20 },
};

beforeEach(() => {
  vi.clearAllMocks();
  api.fetchAgentInsights.mockImplementation((period: string) =>
    Promise.resolve({
      data: insights(period as AgentInsightsPeriod, period === '7d' ? 7 : 24),
      error: null,
    }),
  );
  api.fetchAgentBudgets.mockResolvedValue({ data: BUDGETS, error: null });
});

describe('useAgentsInsights', () => {
  it('starts loading, then reports the period and the budgets', async () => {
    const { result } = renderHook(() => useAgentsInsights('7d', AGENTS, true));
    expect(result.current.status).toBe('loading');
    expect(result.current.insights).toBeUndefined();

    await waitFor(() => expect(result.current.status).toBe('ready'));
    expect(result.current.insights?.byPeriod?.['7d']?.usage.coder?.estimated_cost_usd).toBe(7);
    await waitFor(() => expect(result.current.insights?.budgets).toHaveLength(1));
    expect(result.current.insights?.tenantBudget?.limit).toBe(100);
  });

  it('fetches a period once, when first selected, and keeps it', async () => {
    const { result, rerender } = renderHook(
      ({ period }: { period: '7d' | '24h' }) => useAgentsInsights(period, AGENTS, true),
      { initialProps: { period: '7d' } },
    );
    await waitFor(() => expect(result.current.status).toBe('ready'));
    expect(api.fetchAgentInsights).toHaveBeenCalledTimes(1);

    rerender({ period: '24h' });
    expect(result.current.status).toBe('loading');
    await waitFor(() => expect(result.current.status).toBe('ready'));
    expect(api.fetchAgentInsights).toHaveBeenCalledTimes(2);

    rerender({ period: '7d' });
    expect(result.current.status).toBe('ready');
    expect(api.fetchAgentInsights).toHaveBeenCalledTimes(2);
    expect(Object.keys(result.current.insights?.byPeriod ?? {}).sort()).toEqual(['24h', '7d']);
    expect(api.fetchAgentBudgets).toHaveBeenCalledTimes(1);
  });

  it('leaves a failed part out and retries the period when it is selected again', async () => {
    api.fetchAgentInsights.mockResolvedValueOnce({ data: null, error: 'boom' });
    api.fetchAgentBudgets.mockResolvedValue({ data: null, error: 'boom' });
    const { result, rerender } = renderHook(
      ({ period }: { period: '7d' | '24h' }) => useAgentsInsights(period, AGENTS, true),
      { initialProps: { period: '7d' } },
    );
    await waitFor(() => expect(result.current.status).toBe('failed'));
    expect(result.current.insights).toBeUndefined();

    rerender({ period: '24h' });
    await waitFor(() => expect(result.current.status).toBe('ready'));
    rerender({ period: '7d' });
    await waitFor(() => expect(result.current.status).toBe('ready'));
    expect(result.current.insights?.budgets).toBeUndefined();
  });

  it('does not fetch when the page was handed its insights', async () => {
    renderHook(() => useAgentsInsights('7d', AGENTS, false));
    await act(async () => {});
    expect(api.fetchAgentInsights).not.toHaveBeenCalled();
    expect(api.fetchAgentBudgets).not.toHaveBeenCalled();
  });
});
