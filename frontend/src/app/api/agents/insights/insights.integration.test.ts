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
const CAP = {
  limit_usd: 1000,
  reset_cadence: 'monthly',
  spend_usd: 300,
  resets_at: '2026-11-01T00:00:00Z',
};

function request(query = '') {
  return new NextRequest(`http://localhost/api/agents/insights${query}`);
}

/** Answers every upstream the route calls. */
function upstream(seen: Record<string, string | null> = {}) {
  server.use(
    http.get('http://test-gw/usage/agent-insights', ({ request: r }) => {
      seen.gwPeriod = new URL(r.url).searchParams.get('period');
      seen.gwAuth = r.headers.get('authorization');
      return HttpResponse.json(GATEWAY_INSIGHTS);
    }),
    http.get('http://test-agents/api/v1/run-outcomes', ({ request: r }) => {
      seen.agentsPeriod = new URL(r.url).searchParams.get('period');
      seen.agentsAuth = r.headers.get('authorization');
      return HttpResponse.json({ completed: 4, failed: 1 });
    }),
    http.get('http://test-gw/budgets/tenant-cap', () => HttpResponse.json({ tenant_cap: CAP })),
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

  it('asks the gateway and orbit-agents for the same period with the session token, and adds the cap', async () => {
    const seen = upstream();
    const res = await GET(request('?period=30d'));

    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({
      ...GATEWAY_INSIGHTS,
      outcomes: { completed: 4, failed: 1 },
      tenant_cap: CAP,
    });
    expect(seen).toMatchObject({
      gwPeriod: '30d',
      agentsPeriod: '30d',
      gwAuth: 'Bearer test-access-token',
      agentsAuth: 'Bearer test-access-token',
    });
  });

  it('leaves the cap out for a caller the gateway refuses it, and outcomes out when they cannot be counted', async () => {
    upstream();
    server.use(
      http.get('http://test-gw/budgets/tenant-cap', () =>
        HttpResponse.json({ code: 'forbidden' }, { status: 403 }),
      ),
      http.get('http://test-agents/api/v1/run-outcomes', () =>
        HttpResponse.json({ code: 'internal_error' }, { status: 500 }),
      ),
    );
    const res = await GET(request('?period=30d'));
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual(GATEWAY_INSIGHTS);
  });
});
