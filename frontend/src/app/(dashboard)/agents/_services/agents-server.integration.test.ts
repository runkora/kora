/**
 * Integration tests for fetchAgentsFromBFF, the server-side load behind the
 * /agents list and the dashboard home page.
 *
 * Strategy: `@orbit/server/config` points the agents client at a fake host and
 * MSW answers the upstream calls.
 */
import { describe, it, expect, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { server } from '@orbit/test-utils';

vi.mock('@orbit/server/config', () => ({
  config: { orbitAgentsUrl: 'http://test-agents' },
}));

import { fetchAgentsFromBFF } from './agents-server';

const AGENTS = 'http://test-agents/api/v1';
const STATS = {
  total: 5,
  active: 2,
  completed: 3,
  failed: 0,
  avg_completed_duration_ms: 1000,
  active_by_agent_type: { 'at-1': 2 },
};

describe('fetchAgentsFromBFF', () => {
  it('loads tenant-wide stats and a bounded recent-runs page instead of every run', async () => {
    let runsQuery = new URLSearchParams();
    let statsQuery = new URLSearchParams();
    server.use(
      http.get(`${AGENTS}/agent-types`, () =>
        HttpResponse.json({
          agent_types: [
            {
              id: 'at-1',
              tenant_id: 't-1',
              name: 'Coder',
              slug: 'coder',
              state: 'active',
              framework: 'claude-code',
              tool_scopes: [],
              grant_scopes: [],
              connectors: [],
              mcp_tools: [],
              max_concurrent_runs_per_pod: 10,
              created_at: '2026-09-01T00:00:00Z',
              updated_at: '2026-09-01T00:00:00Z',
            },
          ],
        }),
      ),
      http.get(`${AGENTS}/runs/stats`, ({ request }) => {
        statsQuery = new URL(request.url).searchParams;
        return HttpResponse.json(STATS);
      }),
      http.get(`${AGENTS}/runs`, ({ request }) => {
        runsQuery = new URL(request.url).searchParams;
        return HttpResponse.json({ runs: [] });
      }),
    );

    const result = await fetchAgentsFromBFF('token', 't-1');

    expect(runsQuery.get('limit')).toBe('8');
    expect(statsQuery.has('agent_type_id')).toBe(false);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.agentTypes.map((a) => a.slug)).toEqual(['coder']);
    expect(result.runStats).toEqual(STATS);
  });

  it('falls back to empty stats when upstream returns no body', async () => {
    server.use(
      http.get(`${AGENTS}/agent-types`, () => HttpResponse.json({ agent_types: [] })),
      http.get(`${AGENTS}/runs/stats`, () => new HttpResponse(null, { status: 204 })),
      http.get(`${AGENTS}/runs`, () => HttpResponse.json({ runs: [] })),
    );

    const result = await fetchAgentsFromBFF('token', 't-1');

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.runStats).toEqual({
      total: 0,
      active: 0,
      completed: 0,
      failed: 0,
      avg_completed_duration_ms: null,
    });
  });

  it('returns the upstream status when a call fails', async () => {
    server.use(
      http.get(`${AGENTS}/agent-types`, () => HttpResponse.json({ agent_types: [] })),
      http.get(`${AGENTS}/runs/stats`, () =>
        HttpResponse.json({ code: 'unavailable', message: 'Service Unavailable' }, { status: 503 }),
      ),
      http.get(`${AGENTS}/runs`, () => HttpResponse.json({ runs: [] })),
    );

    const result = await fetchAgentsFromBFF('token', 't-1');

    expect(result).toEqual({ ok: false, status: 503, message: 'Service Unavailable' });
  });
});
