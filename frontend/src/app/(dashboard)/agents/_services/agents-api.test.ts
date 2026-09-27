import { http, HttpResponse } from 'msw';
import { describe, expect, it } from 'vitest';
import { server } from '@orbit/test-utils/msw/server';
import { fetchRunStats } from './agents-api';

/**
 * Unit tests for fetchRunStats, the server-computed run stats the /agents list
 * reads tenant-wide and the agent detail page reads for one agent type.
 */

describe('fetchRunStats', () => {
  const stats = { total: 3, active: 1, completed: 1, failed: 1, avg_completed_duration_ms: 500 };

  it('scopes to an agent type when given one', async () => {
    let seen = new URLSearchParams();
    server.use(
      http.get('/api/agents/runs/stats', ({ request }) => {
        seen = new URL(request.url).searchParams;
        return HttpResponse.json(stats);
      }),
    );

    const result = await fetchRunStats('at-1');

    expect(seen.get('agent_type_id')).toBe('at-1');
    expect(result).toEqual({ data: stats, error: null });
  });

  it('asks for tenant-wide stats without an agent type', async () => {
    let search = 'unset';
    server.use(
      http.get('/api/agents/runs/stats', ({ request }) => {
        search = new URL(request.url).search;
        return HttpResponse.json(stats);
      }),
    );

    await fetchRunStats();

    expect(search).toBe('');
  });

  it('returns the upstream message on an error', async () => {
    server.use(
      http.get('/api/agents/runs/stats', () =>
        HttpResponse.json({ message: 'failed to compute run stats' }, { status: 500 }),
      ),
    );

    expect(await fetchRunStats()).toEqual({ data: null, error: 'failed to compute run stats' });
  });

  it('falls back to the status when the error has no message', async () => {
    server.use(http.get('/api/agents/runs/stats', () => new HttpResponse(null, { status: 502 })));

    expect(await fetchRunStats()).toEqual({ data: null, error: 'Request failed (502)' });
  });
});
