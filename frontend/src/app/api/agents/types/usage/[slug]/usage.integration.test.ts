/**
 * Integration tests for GET /api/agents/types/usage/[slug]
 *
 * Strategy:
 * - `@orbit/server/config` is vi.mock'd to set `inferenceGatewayUrl`.
 * - `@orbit/server/session` is vi.mock'd to control session state.
 * - MSW intercepts calls to `http://test-gw/usage/agent-type/:slug`.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { NextRequest } from 'next/server';
import { server } from '@orbit/test-utils';

vi.mock('@orbit/server/config', () => ({
  config: { inferenceGatewayUrl: 'http://test-gw' },
}));

vi.mock('@orbit/server/session', () => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn().mockReturnValue(true),
}));

import { GET } from './route';
import { getSession, isSessionValid } from '@orbit/server/session';

const USAGE = {
  agent_slug: 'token-probe',
  prompt_tokens: 10,
  completion_tokens: 5,
  cache_read_tokens: 0,
  cache_creation_tokens: 0,
  total_tokens: 15,
  estimated_cost_usd: 0.25,
};

function makeArgs(slug: string) {
  return [
    new NextRequest(`http://localhost/api/agents/types/usage/${slug}`),
    { params: Promise.resolve({ slug }) },
  ] as const;
}

describe('GET /api/agents/types/usage/[slug]', () => {
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

  it('returns 401 when the session is invalid', async () => {
    vi.mocked(isSessionValid).mockReturnValue(false);
    const res = await GET(...makeArgs('token-probe'));
    expect(res.status).toBe(401);
  });

  it('proxies the gateway usage for the slug with the session token', async () => {
    let seen: { slug: string | readonly string[] | undefined; auth: string | null } | null = null;
    server.use(
      http.get('http://test-gw/usage/agent-type/:slug', ({ params, request }) => {
        seen = { slug: params.slug, auth: request.headers.get('authorization') };
        return HttpResponse.json(USAGE);
      }),
    );

    const res = await GET(...makeArgs('token-probe'));
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual(USAGE);
    expect(seen).toEqual({ slug: 'token-probe', auth: 'Bearer test-access-token' });
  });

  it('passes a gateway 400 through', async () => {
    server.use(
      http.get('http://test-gw/usage/agent-type/:slug', () =>
        HttpResponse.json({ code: 'invalid_param' }, { status: 400 }),
      ),
    );
    const res = await GET(...makeArgs('bad_'));
    expect(res.status).toBe(400);
  });
});
