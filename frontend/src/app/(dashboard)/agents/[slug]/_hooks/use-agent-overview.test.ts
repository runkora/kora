import { describe, expect, it } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import type { AgentInsightsPeriod, AgentOverviewResponse } from '@runkora/shared/api';
import { http, HttpResponse } from 'msw';
import { server } from '@runkora/test-utils';
import { useAgentOverview } from './use-agent-overview';

const overview = (period: AgentInsightsPeriod): AgentOverviewResponse => ({
  period,
  runStats: {
    total: 5,
    triggered: 2,
    completed: 4,
    failed: 1,
    durations: {
      median_ms: 1000,
      p95_ms: 1000,
      p99_ms: 1000,
      longest_ms: 1000,
      capped: false,
      buckets: Array<number>(10).fill(0),
      overflow: 0,
    },
    current: {
      total: 5,
      by_state: { completed: 4, failed: 1 },
      live: 0,
      live_by_agent_type: {},
      longest_live_ms: 0,
    },
  },
});

/** Answers the overview route, counting the requests, with each reply in turn. */
function serve(...replies: (() => Response)[]) {
  const requests: string[] = [];
  server.use(
    http.get('/api/agents/types/overview/:slug', ({ request }) => {
      requests.push(new URL(request.url).searchParams.get('period') ?? '');
      return (replies.shift() ?? (() => HttpResponse.json(overview('24h'))))();
    }),
  );
  return requests;
}

const render = (initial?: AgentOverviewResponse) =>
  renderHook(
    ({ period }: { period: AgentInsightsPeriod }) => useAgentOverview('coder', period, initial),
    { initialProps: { period: '7d' as AgentInsightsPeriod } },
  );

describe('useAgentOverview', () => {
  it('is ready at once for the period it was given, fetches another once when selected, and keeps both', async () => {
    const requests = serve();
    const { result, rerender } = render(overview('7d'));
    expect(result.current.status).toBe('ready');
    expect(result.current.overview?.period).toBe('7d');
    expect(requests).toEqual([]);

    rerender({ period: '24h' });
    expect(result.current.status).toBe('loading');
    // The last period stays in view, with its own period, while the new one loads.
    expect(result.current.overview?.period).toBe('7d');
    await waitFor(() => expect(result.current.status).toBe('ready'));
    expect(result.current.overview?.period).toBe('24h');

    rerender({ period: '7d' });
    rerender({ period: '24h' });
    expect(requests).toEqual(['24h']);
  });

  it('shows nothing for a failed period and asks again when it is selected again', async () => {
    serve(() => new Response(null, { status: 503 }));
    const { result, rerender } = render(overview('7d'));

    rerender({ period: '24h' });
    await waitFor(() => expect(result.current.status).toBe('failed'));
    expect(result.current.overview).toBeUndefined();

    rerender({ period: '7d' });
    rerender({ period: '24h' });
    await waitFor(() => expect(result.current.status).toBe('ready'));
  });

  it('asks for nothing without a slug', () => {
    const requests = serve();
    renderHook(() => useAgentOverview(undefined, '7d'));
    expect(requests).toEqual([]);
  });
});
