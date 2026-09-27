/**
 * Integration tests for fetchAgentDetailFromBFF, the server-side load behind
 * the agent detail page's first paint.
 *
 * Strategy: `@orbit/server/config` points the agents client at a fake host,
 * `resolveUserDisplayNames` is vi.mock'd, and MSW answers the upstream calls.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { server } from '@orbit/test-utils';

vi.mock('@orbit/server/config', () => ({
  config: { orbitAgentsUrl: 'http://test-agents' },
}));

vi.mock('@orbit/server/services/identity-server', () => ({
  resolveUserDisplayNames: vi.fn(),
}));

import { fetchAgentDetailFromBFF } from './agent-detail-server';
import { resolveUserDisplayNames } from '@orbit/server/services/identity-server';

const AGENTS = 'http://test-agents/api/v1';
const AGENT_TYPE = {
  id: 'at-1',
  tenant_id: 't-1',
  name: 'Coder',
  slug: 'coder',
  state: 'active',
  framework: 'claude-code',
  identity_mode: 'autonomous',
  tool_scopes: [],
  grant_scopes: [],
  connectors: [],
  mcp_tools: [],
  max_concurrent_runs_per_pod: 10,
  created_at: '2026-09-01T00:00:00Z',
  updated_at: '2026-09-01T00:00:00Z',
};
const STATS = { total: 44, active: 0, completed: 31, failed: 13, avg_completed_duration_ms: 1000 };

function serveAgentType() {
  server.use(
    http.get(`${AGENTS}/agent-types`, () => HttpResponse.json({ agent_types: [AGENT_TYPE] })),
    http.get(`${AGENTS}/agent-types/at-1`, () => HttpResponse.json(AGENT_TYPE)),
  );
}

describe('fetchAgentDetailFromBFF', () => {
  beforeEach(() => {
    vi.mocked(resolveUserDisplayNames).mockResolvedValue(new Map([['user-1', 'Ada Lovelace']]));
  });

  it('loads the first page of 25 runs, its cursor and this agent type’s stats', async () => {
    serveAgentType();
    let runsQuery = new URLSearchParams();
    let statsQuery = new URLSearchParams();
    server.use(
      http.get(`${AGENTS}/runs`, ({ request }) => {
        runsQuery = new URL(request.url).searchParams;
        return HttpResponse.json({
          runs: [
            {
              id: 'run-1',
              tenant_id: 't-1',
              agent_type_id: 'at-1',
              state: 'completed',
              created_by: 'user-1',
              created_at: '2026-09-01T00:00:00Z',
              updated_at: '2026-09-01T00:00:00Z',
            },
          ],
          has_more: true,
          next_cursor: 'c-2',
        });
      }),
      http.get(`${AGENTS}/runs/stats`, ({ request }) => {
        statsQuery = new URL(request.url).searchParams;
        return HttpResponse.json(STATS);
      }),
    );

    const result = await fetchAgentDetailFromBFF('coder', 'token', 't-1');

    expect(runsQuery.get('agent_type_id')).toBe('at-1');
    expect(runsQuery.get('limit')).toBe('25');
    expect(statsQuery.get('agent_type_id')).toBe('at-1');
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.runs.map((r) => r.id)).toEqual(['run-1']);
    expect(result.runs[0].createdByName).toBe('Ada Lovelace');
    expect(result.hasMoreRuns).toBe(true);
    expect(result.nextCursor).toBe('c-2');
    expect(result.runStats).toEqual(STATS);
  });

  it('falls back to empty stats and no further pages when upstream returns no body', async () => {
    serveAgentType();
    server.use(
      http.get(`${AGENTS}/runs`, () => new HttpResponse(null, { status: 204 })),
      http.get(`${AGENTS}/runs/stats`, () => new HttpResponse(null, { status: 204 })),
    );

    const result = await fetchAgentDetailFromBFF('coder', 'token', 't-1');

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.runs).toEqual([]);
    expect(result.hasMoreRuns).toBe(false);
    expect(result.nextCursor).toBeNull();
    expect(result.runStats).toEqual({
      total: 0,
      active: 0,
      completed: 0,
      failed: 0,
      avg_completed_duration_ms: null,
    });
  });

  it('returns 404 for an unknown slug', async () => {
    serveAgentType();

    const result = await fetchAgentDetailFromBFF('no-such-agent', 'token', 't-1');

    expect(result).toEqual({ ok: false, status: 404, message: 'Agent not found' });
  });

  it('surfaces an upstream failure on the stats call', async () => {
    serveAgentType();
    server.use(
      http.get(`${AGENTS}/runs`, () => HttpResponse.json({ runs: [], has_more: false })),
      http.get(`${AGENTS}/runs/stats`, () =>
        HttpResponse.json(
          { code: 'internal_error', message: 'failed to compute run stats' },
          { status: 500 },
        ),
      ),
    );

    const result = await fetchAgentDetailFromBFF('coder', 'token', 't-1');

    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.status).toBe(500);
  });
});
