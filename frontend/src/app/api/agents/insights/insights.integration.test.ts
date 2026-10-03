/**
 * Integration tests for GET /api/agents/insights
 *
 * Strategy:
 * - `@orbit/server/config` is vi.mock'd to point the gateway, orbit-agents and the policy gateway at test hosts.
 * - `@orbit/server/session` is vi.mock'd to control session state.
 * - MSW intercepts the upstream calls.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { NextRequest } from 'next/server';
import { server } from '@orbit/test-utils';

vi.mock('@orbit/server/config', () => ({
  config: {
    inferenceGatewayUrl: 'http://test-gw',
    orbitAgentsUrl: 'http://test-agents',
    policyGatewayUrl: 'http://test-policy',
  },
}));

vi.mock('@orbit/server/session', () => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn().mockReturnValue(true),
}));

import { GET } from './route';
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
  all_delegated_usage: false,
  budgets: [
    {
      agent_slug: 'coder',
      windows: [
        { cadence: 'monthly', limit_usd: 100, spend_usd: 5, resets_at: '2026-11-01T00:00:00Z' },
      ],
    },
  ],
};
const CAP = { limit_usd: 1000, reset_cadence: 'monthly', spend_usd: 300 };

function request(query = '') {
  return new NextRequest(`http://localhost/api/agents/insights${query}`);
}

/** Answers every upstream the route calls, for a session that holds `scopes`. */
function upstream(scopes: string[], seen: Record<string, string | null> = {}) {
  server.use(
    http.get('http://test-gw/usage/agent-insights', ({ request: r }) => {
      const url = new URL(r.url);
      seen.gwPeriod = url.searchParams.get('period');
      seen.gwEnd = url.searchParams.get('end');
      seen.gwAuth = r.headers.get('authorization');
      return HttpResponse.json(GATEWAY_INSIGHTS);
    }),
    http.get('http://test-agents/api/v1/run-outcomes', ({ request: r }) => {
      const url = new URL(r.url);
      seen.agentsPeriod = url.searchParams.get('period');
      seen.agentsEnd = url.searchParams.get('end');
      seen.agentsAuth = r.headers.get('authorization');
      return HttpResponse.json({ completed: 4, failed: 1 });
    }),
    http.get('http://test-policy/policy/scopes', () => HttpResponse.json({ scopes })),
    http.get('http://test-gw/budgets/tenant-cap', () => {
      seen.capCalled = 'yes';
      return HttpResponse.json({ tenant_cap: CAP });
    }),
  );
  return seen;
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
    expect((await GET(request())).status).toBe(401);
  });

  it('rejects an unknown period, including an inherited object key', async () => {
    expect((await GET(request('?period=1y'))).status).toBe(400);
    expect((await GET(request('?period=constructor'))).status).toBe(400);
  });

  it('sends the gateway and orbit-agents the same period and end time, with the session token', async () => {
    const seen = upstream([]);
    const res = await GET(request('?period=30d'));

    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({
      ...GATEWAY_INSIGHTS,
      outcomes: { completed: 4, failed: 1 },
    });
    expect(seen.gwPeriod).toBe('30d');
    expect(seen.agentsPeriod).toBe('30d');
    expect(seen.gwEnd).toBeTruthy();
    expect(seen.gwEnd).toBe(seen.agentsEnd);
    expect(seen.gwAuth).toBe('Bearer test-access-token');
    expect(seen.agentsAuth).toBe('Bearer test-access-token');
  });

  it('defaults to 7d', async () => {
    const seen = upstream([]);
    await GET(request());
    expect(seen.gwPeriod).toBe('7d');
  });

  it('adds the tenant cap only for a session that holds platform:budgets:read', async () => {
    const without = upstream(['agents:types:read']);
    const noCap = await (await GET(request())).json();
    expect('tenant_cap' in noCap).toBe(false);
    expect(without.capCalled).toBeUndefined();

    const withScope = upstream(['agents:types:read', 'platform:budgets:read']);
    const body = await (await GET(request())).json();
    expect(body.tenant_cap).toEqual(CAP);
    expect(withScope.capCalled).toBe('yes');
  });

  it('leaves the cap out when the scopes cannot be read', async () => {
    const seen = upstream([]);
    server.use(
      http.get('http://test-policy/policy/scopes', () => HttpResponse.json({}, { status: 503 })),
    );
    const body = await (await GET(request())).json();
    expect('tenant_cap' in body).toBe(false);
    expect(seen.capCalled).toBeUndefined();
  });

  it('still answers when orbit-agents cannot count runs', async () => {
    upstream([]);
    server.use(
      http.get('http://test-agents/api/v1/run-outcomes', () =>
        HttpResponse.json({ code: 'internal_error' }, { status: 500 }),
      ),
    );
    const res = await GET(request('?period=30d'));
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual(GATEWAY_INSIGHTS);
  });

  it('passes the gateway status through when usage is unavailable', async () => {
    upstream([]);
    server.use(
      http.get('http://test-gw/usage/agent-insights', () =>
        HttpResponse.json({ code: 'agent_insights_unavailable' }, { status: 503 }),
      ),
    );
    const res = await GET(request());
    expect(res.status).toBe(503);
    expect(await res.json()).toMatchObject({ code: 'insights_unavailable' });
  });
});
