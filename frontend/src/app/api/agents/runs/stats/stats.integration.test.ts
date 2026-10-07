/** Integration tests for GET /api/agents/runs/stats. MSW stands in for orbit-agents. */
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
import { getSession } from '@orbit/server/session';

const STATS_URL = 'http://test-agents/api/v1/runs/stats';
const get = (query = '') => GET(new NextRequest(`http://localhost/api/agents/runs/stats${query}`));

describe('GET /api/agents/runs/stats', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockResolvedValue({
      accessToken: 'test-access-token',
      tenantId: 'test-tenant',
    } as never);
  });

  it('asks orbit-agents for the period and returns its stats', async () => {
    let period: string | null = null;
    server.use(
      http.get(STATS_URL, ({ request }) => {
        period = new URL(request.url).searchParams.get('period');
        return HttpResponse.json({ total: 3, current: { total: 9, live: 2 } });
      }),
    );

    const res = await get('?period=30d');
    const body = await res.json();

    expect(res.status).toBe(200);
    expect(period).toBe('30d');
    expect(body.data.current.live).toBe(2);
  });

  it('defaults to the 7 day window', async () => {
    let period: string | null = null;
    server.use(
      http.get(STATS_URL, ({ request }) => {
        period = new URL(request.url).searchParams.get('period');
        return HttpResponse.json({ total: 0 });
      }),
    );
    await get();
    expect(period).toBe('7d');
  });

  it('rejects an unknown period without calling upstream', async () => {
    const upstream = vi.fn();
    server.use(
      http.get(STATS_URL, () => {
        upstream();
        return HttpResponse.json({});
      }),
    );

    const res = await get('?period=1y');

    expect(res.status).toBe(400);
    expect(upstream).not.toHaveBeenCalled();
  });

  it('passes an upstream error through with its status', async () => {
    server.use(
      http.get(STATS_URL, () =>
        HttpResponse.json({ code: 'internal_error', message: 'failed' }, { status: 500 }),
      ),
    );
    expect((await get()).status).toBe(500);
  });
});
