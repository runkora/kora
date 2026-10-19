import { describe, expect, it } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import type { AgentInsightsPeriod, AgentInsightsResponse } from '@runkora/shared/api';
import { http, HttpResponse } from 'msw';
import { server } from '@runkora/test-utils';
import { useAgentsInsights } from './use-agents-insights';

const AGENTS = [
  { slug: 'coder', name: 'Coder' },
  { slug: 'notes', name: 'Notes' },
];

const budgetWindow = (limit_usd: number, spend_usd: number) => ({
  cadence: 'monthly' as const,
  limit_usd,
  spend_usd,
  resets_at: '2026-11-01T00:00:00Z',
});

function insights(period: AgentInsightsPeriod, cost: number): AgentInsightsResponse {
  return {
    period,
    usage: [],
    total_cost_usd: cost,
    previous_total_usd: 0,
    models: [],
    trend: [{ start: '2026-09-27T00:00:00Z', cost_usd: cost }],
    budgets: [
      { agent_slug: 'coder', windows: [budgetWindow(10, 2)] },
      { agent_slug: 'notes', windows: [budgetWindow(10, 9)] },
    ],
  };
}

/** Answers the insights route, counting the requests, with each reply in turn. */
function serve(...replies: (() => Response)[]) {
  const requests: string[] = [];
  server.use(
    http.get('/api/agents/insights', ({ request }) => {
      requests.push(new URL(request.url).searchParams.get('period') ?? '');
      return (replies.shift() ?? (() => HttpResponse.json(insights('24h', 24))))();
    }),
  );
  return requests;
}

describe('useAgentsInsights', () => {
  it('is ready at once for the period it was given, fetches another once when selected, and keeps both', async () => {
    const requests = serve();
    const { result, rerender } = renderHook(
      ({ period }: { period: AgentInsightsPeriod }) =>
        useAgentsInsights(period, AGENTS, insights('7d', 7)),
      { initialProps: { period: '7d' as AgentInsightsPeriod } },
    );
    expect(result.current.status).toBe('ready');
    // Budgets come from the loaded response, the closest to a limit first, delegated agents included.
    expect(result.current.insights?.budgets?.map((b) => b.name)).toEqual(['Notes', 'Coder']);
    expect(requests).toEqual([]);

    rerender({ period: '24h' });
    expect(result.current.status).toBe('loading');
    await waitFor(() => expect(result.current.status).toBe('ready'));
    rerender({ period: '7d' });
    rerender({ period: '24h' });
    expect(requests).toEqual(['24h']);
    expect(Object.keys(result.current.insights?.byPeriod ?? {}).sort()).toEqual(['24h', '7d']);
  });

  it('leaves a failed period out and asks again when it is selected again', async () => {
    serve(() => new Response(null, { status: 503 }));
    const { result, rerender } = renderHook(
      ({ period }: { period: AgentInsightsPeriod }) => useAgentsInsights(period, AGENTS),
      { initialProps: { period: '24h' as AgentInsightsPeriod } },
    );
    await waitFor(() => expect(result.current.status).toBe('failed'));
    expect(result.current.insights).toBeUndefined();

    rerender({ period: '7d' });
    rerender({ period: '24h' });
    await waitFor(() => expect(result.current.status).toBe('ready'));
  });
});
