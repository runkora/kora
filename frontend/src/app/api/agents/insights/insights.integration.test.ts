/**
 * Integration tests for GET /api/agents/insights and /api/agents/insights/budgets
 *
 * Strategy:
 * - `@orbit/server/config` is vi.mock'd to point the gateway and orbit-agents at test hosts.
 * - `@orbit/server/session` is vi.mock'd to control session state.
 * - MSW intercepts the upstream calls.
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

import { GET as getInsights } from './route';
import { GET as getBudgets } from './budgets/route';
import { getSession, isSessionValid } from '@orbit/server/session';

const GATEWAY_INSIGHTS = {
  period: '30d',
  start: '2026-09-04T00:00:00Z',
  end: '2026-10-04T00:00:00Z',
  bucket_seconds: 259200,
  usage: [],
  total_cost_usd: 0,
  previous_total_usd: 0,
  models: [],
  trend: [],
};

function insightsRequest(query = '') {
  return new NextRequest(`http://localhost/api/agents/insights${query}`);
}

beforeEach(() => {
  vi.mocked(getSession).mockResolvedValue({
    user: { sub: 'test-sub', email: 'test@orbit.local', name: 'Test' },
    accessToken: 'test-access-token',
    refreshToken: 'test-refresh',
    expiresAt: Math.floor(Date.now() / 1000) + 3600,
    tenantId: 'test-tenant',
  } as never);
  vi.mocked(isSessionValid).mockReturnValue(true);
});

describe('GET /api/agents/insights', () => {
  it('returns 401 when the session is invalid', async () => {
    vi.mocked(isSessionValid).mockReturnValue(false);
    expect((await getInsights(insightsRequest())).status).toBe(401);
  });

  it('rejects an unknown period without calling upstream', async () => {
    const res = await getInsights(insightsRequest('?period=1y'));
    expect(res.status).toBe(400);
  });

  it('asks the gateway and orbit-agents for the same period with the session token', async () => {
    const seen: Record<string, string | null> = {};
    server.use(
      http.get('http://test-gw/usage/agent-insights', ({ request }) => {
        seen.gwPeriod = new URL(request.url).searchParams.get('period');
        seen.gwAuth = request.headers.get('authorization');
        return HttpResponse.json(GATEWAY_INSIGHTS);
      }),
      http.get('http://test-agents/api/v1/run-outcomes', ({ request }) => {
        seen.agentsPeriod = new URL(request.url).searchParams.get('period');
        seen.agentsAuth = request.headers.get('authorization');
        return HttpResponse.json({ completed: 4, failed: 1 });
      }),
    );

    const res = await getInsights(insightsRequest('?period=30d'));
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({
      ...GATEWAY_INSIGHTS,
      outcomes: { completed: 4, failed: 1 },
    });
    expect(seen).toEqual({
      gwPeriod: '30d',
      gwAuth: 'Bearer test-access-token',
      agentsPeriod: '30d',
      agentsAuth: 'Bearer test-access-token',
    });
  });

  it('defaults to 7d', async () => {
    let gwPeriod: string | null = null;
    server.use(
      http.get('http://test-gw/usage/agent-insights', ({ request }) => {
        gwPeriod = new URL(request.url).searchParams.get('period');
        return HttpResponse.json(GATEWAY_INSIGHTS);
      }),
      http.get('http://test-agents/api/v1/run-outcomes', () =>
        HttpResponse.json({ completed: 0, failed: 0 }),
      ),
    );
    await getInsights(insightsRequest());
    expect(gwPeriod).toBe('7d');
  });

  it('still answers when orbit-agents cannot count runs', async () => {
    server.use(
      http.get('http://test-gw/usage/agent-insights', () => HttpResponse.json(GATEWAY_INSIGHTS)),
      http.get('http://test-agents/api/v1/run-outcomes', () =>
        HttpResponse.json({ code: 'internal_error' }, { status: 500 }),
      ),
    );
    const res = await getInsights(insightsRequest('?period=30d'));
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual(GATEWAY_INSIGHTS);
  });

  it('passes the gateway status through when usage is unavailable', async () => {
    server.use(
      http.get('http://test-gw/usage/agent-insights', () =>
        HttpResponse.json({ code: 'agent_visibility_unavailable' }, { status: 503 }),
      ),
      http.get('http://test-agents/api/v1/run-outcomes', () =>
        HttpResponse.json({ completed: 1, failed: 0 }),
      ),
    );
    expect((await getInsights(insightsRequest())).status).toBe(503);
  });
});

describe('GET /api/agents/insights/budgets', () => {
  const spend = {
    week_start: '2026-09-28T00:00:00Z',
    month_start: '2026-10-01T00:00:00Z',
    agents: [{ agent_slug: 'coder', week_to_date_usd: 2, month_to_date_usd: 5 }],
  };
  const limits = {
    data: [
      { agent_slug: 'coder', limit_usd: 50, reset_cadence: 'monthly' },
      { agent_slug: 'idle', limit_usd: 10, reset_cadence: 'weekly' },
    ],
  };
  const cap = { tenant_cap: { limit_usd: 1000, reset_cadence: 'monthly', spend_usd: 300 } };

  function upstream(principals: () => Response) {
    server.use(
      http.get('http://test-gw/usage/agent-budgets', () => HttpResponse.json(spend)),
      http.get('http://test-agents/api/v1/agent-budgets', () => HttpResponse.json(limits)),
      http.get('http://test-gw/budgets/tenant-cap', principals),
    );
  }

  it('joins spend to limits and includes the tenant cap for a caller who may read it', async () => {
    upstream(() => HttpResponse.json(cap));
    const res = await getBudgets(new NextRequest('http://localhost/api/agents/insights/budgets'));
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({
      agents: [
        {
          agent_slug: 'coder',
          week_to_date_usd: 2,
          month_to_date_usd: 5,
          limit_usd: 50,
          reset_cadence: 'monthly',
        },
        {
          agent_slug: 'idle',
          week_to_date_usd: 0,
          month_to_date_usd: 0,
          limit_usd: 10,
          reset_cadence: 'weekly',
        },
      ],
      ...cap,
    });
  });

  it('leaves the tenant cap out for a caller without platform:budgets:read', async () => {
    upstream(() => HttpResponse.json({ code: 'forbidden' }, { status: 403 }));
    const res = await getBudgets(new NextRequest('http://localhost/api/agents/insights/budgets'));
    expect(res.status).toBe(200);
    expect('tenant_cap' in (await res.json())).toBe(false);
  });

  it('fails when the limits cannot be read, rather than show spend as unbudgeted', async () => {
    server.use(
      http.get('http://test-gw/usage/agent-budgets', () => HttpResponse.json(spend)),
      http.get('http://test-agents/api/v1/agent-budgets', () =>
        HttpResponse.json({ code: 'internal_error' }, { status: 500 }),
      ),
      http.get('http://test-gw/budgets/tenant-cap', () => HttpResponse.json(cap)),
    );
    const res = await getBudgets(new NextRequest('http://localhost/api/agents/insights/budgets'));
    expect(res.status).toBe(500);
    expect(await res.json()).toMatchObject({
      code: 'budgets_unavailable',
      message: expect.any(String),
    });
  });
});
