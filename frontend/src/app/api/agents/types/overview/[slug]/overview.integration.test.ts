/**
 * Integration tests for GET /api/agents/types/overview/[slug]. `@orbit/server/config`
 * and `@orbit/server/session` are mocked, and MSW answers the upstream calls.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { NextRequest } from 'next/server';
import { server } from '@orbit/test-utils';

vi.mock('@orbit/server/config', () => ({
  config: { inferenceGatewayUrl: 'http://test-gw', orbitAgentsUrl: 'http://test-agents' },
}));

vi.mock('@orbit/server/session', () => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn().mockReturnValue(true),
}));

import { GET } from './route';
import { getSession } from '@orbit/server/session';

const INSIGHTS = { period: '30d', usage: [], total_cost_usd: 0, trend: [], models: [] };
const RUN_STATS = { total: 3, triggered: 1, completed: 2, failed: 1 };

const call = (query = '?period=30d', slug = 'token-probe') =>
  GET(new NextRequest(`http://localhost/api/agents/types/overview/${slug}${query}`), {
    params: Promise.resolve({ slug }),
  });

beforeEach(() => {
  vi.mocked(getSession).mockResolvedValue({ accessToken: 'test-access-token' } as never);
});

describe('GET /api/agents/types/overview/[slug]', () => {
  it('rejects an unknown period, including an inherited object key', async () => {
    expect((await call('?period=1y')).status).toBe(400);
    expect((await call('?period=constructor')).status).toBe(400);
  });

  it('reads the gateway and orbit-agents for the slug and period with the session token', async () => {
    const seen: Record<string, string> = {};
    server.use(
      http.get('http://test-gw/usage/agent-type/:slug', ({ params, request: r }) => {
        seen.gw = `${String(params.slug)} ${new URL(r.url).searchParams.get('period')} ${r.headers.get('authorization')}`;
        return HttpResponse.json(INSIGHTS);
      }),
      http.get('http://test-agents/api/v1/run-stats/:slug', ({ params, request: r }) => {
        seen.agents = `${String(params.slug)} ${new URL(r.url).searchParams.get('period')} ${r.headers.get('authorization')}`;
        return HttpResponse.json(RUN_STATS);
      }),
    );
    const res = await call();

    expect(await res.json()).toEqual({ period: '30d', insights: INSIGHTS, runStats: RUN_STATS });
    expect(seen).toEqual({
      gw: 'token-probe 30d Bearer test-access-token',
      agents: 'token-probe 30d Bearer test-access-token',
    });
  });

  it('leaves out a part the caller may not read, a 403 or a 404, and still returns the other', async () => {
    server.use(
      http.get('http://test-gw/usage/agent-type/:slug', () =>
        HttpResponse.json({}, { status: 403 }),
      ),
      http.get('http://test-agents/api/v1/run-stats/:slug', () => HttpResponse.json(RUN_STATS)),
    );
    expect(await (await call()).json()).toEqual({ period: '30d', runStats: RUN_STATS });

    server.use(
      http.get('http://test-gw/usage/agent-type/:slug', () => HttpResponse.json(INSIGHTS)),
      http.get('http://test-agents/api/v1/run-stats/:slug', () =>
        HttpResponse.json({}, { status: 404 }),
      ),
    );
    expect(await (await call()).json()).toEqual({ period: '30d', insights: INSIGHTS });
  });

  it('names a part that errored instead of leaving it out silently', async () => {
    server.use(
      http.get('http://test-gw/usage/agent-type/:slug', () =>
        HttpResponse.json({}, { status: 503 }),
      ),
      http.get('http://test-agents/api/v1/run-stats/:slug', () =>
        HttpResponse.json({}, { status: 500 }),
      ),
    );
    expect(await (await call()).json()).toEqual({
      period: '30d',
      failed: ['insights', 'runStats'],
    });
  });
});
