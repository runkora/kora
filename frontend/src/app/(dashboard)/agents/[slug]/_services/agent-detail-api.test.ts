import { http, HttpResponse } from 'msw';
import { describe, expect, it } from 'vitest';
import { server } from '@orbit/test-utils/msw/server';
import { fetchAgentRuns } from './agent-detail-api';
import type { AgentTypeRow } from '../../_types';

/**
 * Unit tests for fetchAgentRuns, one page of an agent type's runs with its
 * cursor, as the agent detail page's runs tab reads them.
 */

const agent = { id: 'at-1', name: 'Coder', slug: 'coder' } as AgentTypeRow;

describe('fetchAgentRuns', () => {
  it('sends cursor and limit and returns the page with its cursor', async () => {
    let seen = new URLSearchParams();
    server.use(
      http.get('/api/agents/types/at-1/runs', ({ request }) => {
        seen = new URL(request.url).searchParams;
        return HttpResponse.json({
          data: [
            {
              id: 'run-1',
              tenant_id: 't-1',
              agent_type_id: 'at-1',
              state: 'failed',
              created_by: 'user-1',
              created_at: '2026-09-01T00:00:00Z',
              updated_at: '2026-09-01T00:00:00Z',
            },
          ],
          has_more: true,
          next_cursor: 'c-2',
        });
      }),
    );

    const page = await fetchAgentRuns('at-1', agent, { cursor: 'c-1', limit: 25 });

    expect(seen.get('cursor')).toBe('c-1');
    expect(seen.get('limit')).toBe('25');
    expect(page.error).toBeNull();
    expect(page.data.map((r) => r.id)).toEqual(['run-1']);
    expect(page.data[0].agentTypeSlug).toBe('coder');
    expect(page.hasMore).toBe(true);
    expect(page.nextCursor).toBe('c-2');
  });

  it('sends no query string for the first page with default size', async () => {
    let search = 'unset';
    server.use(
      http.get('/api/agents/types/at-1/runs', ({ request }) => {
        search = new URL(request.url).search;
        return HttpResponse.json({ data: [] });
      }),
    );

    const page = await fetchAgentRuns('at-1', agent);

    expect(search).toBe('');
    expect(page.hasMore).toBe(false);
    expect(page.nextCursor).toBeNull();
  });

  it('returns the upstream message and no further pages on an error', async () => {
    server.use(
      http.get('/api/agents/types/at-1/runs', () =>
        HttpResponse.json({ message: 'Service Unavailable' }, { status: 503 }),
      ),
    );

    const page = await fetchAgentRuns('at-1', agent, { cursor: 'c-1' });

    expect(page.error).toBe('Service Unavailable');
    expect(page.data).toEqual([]);
    expect(page.hasMore).toBe(false);
  });

  it('reports a network error', async () => {
    server.use(http.get('/api/agents/types/at-1/runs', () => HttpResponse.error()));

    const page = await fetchAgentRuns('at-1', agent);

    expect(page.error).toBe('Network error: could not reach the server.');
  });
});
