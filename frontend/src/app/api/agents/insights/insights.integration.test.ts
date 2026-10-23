/**
 * Integration tests for GET /api/agents/insights. `@runkora/server/config` and
 * `@runkora/server/session` are mocked, and MSW answers the upstream calls.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { NextRequest } from 'next/server';
import { server } from '@runkora/test-utils';

vi.mock('@runkora/server/config', () => ({
  config: { inferenceGatewayUrl: 'http://test-gw', koraAgentsUrl: 'http://test-agents' },
}));

vi.mock('@runkora/server/session', () => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn().mockReturnValue(true),
}));

import { GET } from './route';
import { getSession } from '@runkora/server/session';

const INSIGHTS = { period: '30d', usage: [], total_cost_usd: 0, trend: [] };

const request = (query = '') => new NextRequest(`http://localhost/api/agents/insights${query}`);

beforeEach(() => {
  vi.mocked(getSession).mockResolvedValue({ accessToken: 'test-access-token' } as never);
});

describe('GET /api/agents/insights', () => {
  it('rejects an unknown period, including an inherited object key', async () => {
    expect((await GET(request('?period=1y'))).status).toBe(400);
    expect((await GET(request('?period=constructor'))).status).toBe(400);
  });

  it('asks the gateway and kora-agents for the same period with the session token', async () => {
    const seen: Record<string, string | null> = {};
    server.use(
      http.get('http://test-gw/usage/agent-insights', ({ request: r }) => {
        seen.gw = `${new URL(r.url).searchParams.get('period')} ${r.headers.get('authorization')}`;
        return HttpResponse.json(INSIGHTS);
      }),
      http.get('http://test-agents/api/v1/run-outcomes', ({ request: r }) => {
        seen.agents = `${new URL(r.url).searchParams.get('period')} ${r.headers.get('authorization')}`;
        return HttpResponse.json({ completed: 4, failed: 1 });
      }),
    );
    const res = await GET(request('?period=30d'));

    expect(await res.json()).toEqual({ ...INSIGHTS, outcomes: { completed: 4, failed: 1 } });
    expect(seen).toEqual({
      gw: '30d Bearer test-access-token',
      agents: '30d Bearer test-access-token',
    });
  });

  it('leaves outcomes out when they cannot be counted', async () => {
    server.use(
      http.get('http://test-gw/usage/agent-insights', () => HttpResponse.json(INSIGHTS)),
      http.get('http://test-agents/api/v1/run-outcomes', () =>
        HttpResponse.json({}, { status: 500 }),
      ),
    );
    const res = await GET(request('?period=30d'));
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual(INSIGHTS);
  });

  it('passes the gateway status through when the rollups are refused', async () => {
    server.use(
      http.get('http://test-gw/usage/agent-insights', () => HttpResponse.json({}, { status: 403 })),
      http.get('http://test-agents/api/v1/run-outcomes', () => HttpResponse.json({})),
    );
    expect((await GET(request())).status).toBe(403);
  });
});
