/**
 * Integration tests for GET /api/agents/runs/stats.
 *
 * Strategy: `@orbit/server/config` and `@orbit/server/session` are vi.mock'd,
 * and MSW intercepts the upstream `/api/v1/runs/stats` call.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { NextRequest } from 'next/server';
import { server } from '@orbit/test-utils';

vi.mock('@orbit/server/config', () => ({
  config: { orbitAgentsUrl: 'http://test-agents' },
}));

vi.mock('@orbit/server/session', () => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn().mockReturnValue(true),
}));

import { GET } from './route';
import { getSession, isSessionValid } from '@orbit/server/session';

const STATS_URL = 'http://test-agents/api/v1/runs/stats';

const STATS = {
  total: 44,
  active: 0,
  completed: 31,
  failed: 13,
  avg_completed_duration_ms: 1_230_918,
};

function get(query = ''): Promise<Response> {
  return GET(new NextRequest(`http://localhost/api/agents/runs/stats${query}`));
}

describe('GET /api/agents/runs/stats', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockResolvedValue({
      accessToken: 'test-access-token',
      tenantId: 'test-tenant',
    } as never);
    vi.mocked(isSessionValid).mockReturnValue(true);
  });

  it('forwards agent_type_id and returns the stats unchanged', async () => {
    let seen = new URLSearchParams();
    server.use(
      http.get(STATS_URL, ({ request }) => {
        seen = new URL(request.url).searchParams;
        return HttpResponse.json(STATS);
      }),
    );

    const res = await get('?agent_type_id=at-1');

    expect(res.status).toBe(200);
    expect(await res.json()).toEqual(STATS);
    expect(seen.get('agent_type_id')).toBe('at-1');
    expect(seen.get('tenant_id')).toBe('test-tenant');
  });

  it('asks for tenant-wide stats when no agent type is given', async () => {
    let seen = new URLSearchParams();
    server.use(
      http.get(STATS_URL, ({ request }) => {
        seen = new URL(request.url).searchParams;
        return HttpResponse.json({ ...STATS, active_by_agent_type: { 'at-1': 2 } });
      }),
    );

    const body = await (await get()).json();

    expect(seen.has('agent_type_id')).toBe(false);
    expect(body.active_by_agent_type).toEqual({ 'at-1': 2 });
  });

  it('passes an upstream error through with its status', async () => {
    server.use(
      http.get(STATS_URL, () =>
        HttpResponse.json(
          { code: 'internal_error', message: 'failed to compute run stats' },
          { status: 500 },
        ),
      ),
    );

    const res = await get();

    expect(res.status).toBe(500);
    expect((await res.json()).message).toBe('failed to compute run stats');
  });
});
