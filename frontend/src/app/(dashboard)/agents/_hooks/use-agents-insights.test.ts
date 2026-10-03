import { describe, expect, it } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import type { AgentInsightsPeriod, AgentInsightsResponse } from '@orbit/shared/api';
import { http, HttpResponse } from 'msw';
import { server } from '@orbit/test-utils';
import { useAgentsInsights } from './use-agents-insights';

const AGENTS = [
  { slug: 'coder', name: 'Coder', identityMode: 'autonomous' },
  { slug: 'notes', name: 'Notes', identityMode: 'delegated' },
];

const window = (limit_usd: number, spend_usd: number) => ({
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
    all_delegated_usage: false,
    budgets: [
      { agent_slug: 'coder', windows: [window(10, 2)] },
      { agent_slug: 'notes', windows: [window(10, 9)] },
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

type Period = '7d' | '24h';

describe('useAgentsInsights', () => {
  it('is ready at once for the period it was given, fetches another once when selected, and keeps both', async () => {
    const requests = serve();
    const { result, rerender } = renderHook(
      ({ period }: { period: Period }) =>
        useAgentsInsights(period, AGENTS, true, insights('7d', 7)),
      { initialProps: { period: '7d' } },
    );
    expect(result.current.status).toBe('ready');
    // A delegated agent has no budget row, whatever the gateway sent.
    expect(result.current.insights?.budgets?.map((b) => b.name)).toEqual(['Coder']);
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
      ({ period }: { period: Period }) => useAgentsInsights(period, AGENTS, true),
      { initialProps: { period: '24h' } },
    );
    await waitFor(() => expect(result.current.status).toBe('failed'));
    expect(result.current.insights).toBeUndefined();

    rerender({ period: '7d' });
    rerender({ period: '24h' });
    await waitFor(() => expect(result.current.status).toBe('ready'));
  });
});
