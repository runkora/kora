import { http, HttpResponse } from 'msw';
import { describe, expect, it } from 'vitest';
import { server } from '@orbit/test-utils/msw/server';
import { fetchRuns, fetchRunStats } from './agents-api';
import type { AgentTypeRow } from '../_types';

/**
 * Unit tests for the tenant-wide run reads the /agents list and the dashboard
 * use: one page of runs with its cursor, and the server-computed run stats.
 */

const agentTypes = [{ id: 'at-1', name: 'Coder', slug: 'coder' } as AgentTypeRow];

function run(id: string) {
  return {
    id,
    tenant_id: 't-1',
    agent_type_id: 'at-1',
    state: 'completed',
    created_by: 'user-1',
    created_at: '2026-09-01T00:00:00Z',
    updated_at: '2026-09-01T00:00:00Z',
  };
}

describe('fetchRuns', () => {
  it('sends agent_type_id, cursor and limit, and returns the page with its cursor', async () => {
    let seen = new URLSearchParams();
    server.use(
      http.get('/api/agents/runs', ({ request }) => {
        seen = new URL(request.url).searchParams;
        return HttpResponse.json({
          data: [run('run-1')],
          has_more: true,
          next_cursor: 'c-2',
        });
      }),
    );

    const page = await fetchRuns(agentTypes, { agentTypeId: 'at-1', cursor: 'c-1', limit: 25 });

    expect(seen.get('agent_type_id')).toBe('at-1');
    expect(seen.get('cursor')).toBe('c-1');
    expect(seen.get('limit')).toBe('25');
    expect(page.error).toBeNull();
    expect(page.data.map((r) => r.id)).toEqual(['run-1']);
    expect(page.data[0].agentTypeName).toBe('Coder');
    expect(page.hasMore).toBe(true);
    expect(page.nextCursor).toBe('c-2');
  });

  it('sends no query string when no options are given', async () => {
    let search = 'unset';
    server.use(
      http.get('/api/agents/runs', ({ request }) => {
        search = new URL(request.url).search;
        return HttpResponse.json({ data: [] });
      }),
    );

    const page = await fetchRuns(agentTypes);

    expect(search).toBe('');
    expect(page.hasMore).toBe(false);
    expect(page.nextCursor).toBeNull();
  });

  it('returns the upstream message and no further pages on an error', async () => {
    server.use(
      http.get('/api/agents/runs', () =>
        HttpResponse.json({ message: 'Service Unavailable' }, { status: 503 }),
      ),
    );

    const page = await fetchRuns(agentTypes, { cursor: 'c-1' });

    expect(page.error).toBe('Service Unavailable');
    expect(page.data).toEqual([]);
    expect(page.hasMore).toBe(false);
    expect(page.nextCursor).toBeNull();
  });

  it('reports a network error', async () => {
    server.use(http.get('/api/agents/runs', () => HttpResponse.error()));

    const page = await fetchRuns(agentTypes);

    expect(page.error).toBe('Network error: could not reach the server.');
    expect(page.hasMore).toBe(false);
  });
});

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
