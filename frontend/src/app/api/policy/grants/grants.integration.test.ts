/**
 * Integration tests for GET /api/policy/grants and POST /api/policy/grants
 *
 * Strategy:
 * - `@orbit/server/config` is vi.mock'd to set `policyServiceUrl` without
 *   triggering `requireEnv` at import time.
 * - `@orbit/server/session` is vi.mock'd so `getSession` and `isSessionValid`
 *   return controllable state without a real iron-session cookie.
 * - MSW intercepts calls to `http://test-policy-svc/policy/grants`.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { NextRequest } from 'next/server';
import { server } from '@orbit/test-utils';

// ── Module mocks ──────────────────────────────────────────────────────────────

vi.mock('@orbit/server/config', () => ({
  config: { policyServiceUrl: 'http://test-policy-svc' },
}));

vi.mock('@orbit/server/session', () => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn().mockReturnValue(true),
}));

// ── Import under test ─────────────────────────────────────────────────────────

import { GET, POST } from './route';
import { getSession, isSessionValid } from '@orbit/server/session';

// ── Helpers ───────────────────────────────────────────────────────────────────

const GRANTS_URL = 'http://test-policy-svc/policy/grants';

function makeSession(overrides: Record<string, unknown> = {}) {
  return {
    user: { sub: 'test-sub', email: 'test@orbit.local', name: 'Test' },
    accessToken: 'test-access-token',
    refreshToken: 'test-refresh',
    expiresAt: Math.floor(Date.now() / 1000) + 3600,
    tenantId: 'test-tenant',
    permissions: { scopes: ['platform:grants:read'], evaluatedAt: 0 },
    ...overrides,
  };
}

function makePostRequest(body: unknown): NextRequest {
  return new NextRequest('http://localhost/api/policy/grants', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

const GRANTS_FIXTURE = {
  grants: [
    {
      id: 'grant-1',
      tenant_id: 'test-tenant',
      principal_type: 'HUMAN',
      principal_id: 'user-1',
      scopes: ['platform:grants:read'],
      created_at: '2026-01-01T00:00:00Z',
    },
  ],
  total: 1,
  limit: 50,
  offset: 0,
};

const GRANT_FIXTURE = {
  id: 'grant-new',
  tenant_id: 'test-tenant',
  principal_type: 'HUMAN',
  principal_id: 'user-2',
  scopes: ['platform:grants:read'],
  created_at: '2026-01-01T00:00:00Z',
};

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('GET /api/policy/grants', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockResolvedValue(
      makeSession() as ReturnType<typeof makeSession> as never,
    );
    vi.mocked(isSessionValid).mockReturnValue(true);
  });

  it('returns 401 when the session is invalid', async () => {
    vi.mocked(isSessionValid).mockReturnValue(false);
    const res = await GET();
    expect(res.status).toBe(401);
  });

  it('returns 200 with grants data on success', async () => {
    server.use(http.get(GRANTS_URL, () => HttpResponse.json(GRANTS_FIXTURE)));

    const res = await GET();
    expect(res.status).toBe(200);
    const body = (await res.json()) as { data: typeof GRANTS_FIXTURE };
    expect(body.data).toEqual(GRANTS_FIXTURE);
  });

  it('maps an upstream 403 to 403 with upstream_error', async () => {
    server.use(http.get(GRANTS_URL, () => new HttpResponse('Forbidden', { status: 403 })));

    const res = await GET();
    expect(res.status).toBe(403);
    const body = (await res.json()) as { code: string };
    expect(body.code).toBe('upstream_error');
  });

  it('maps an upstream 5xx to 502', async () => {
    server.use(
      http.get(GRANTS_URL, () => new HttpResponse('Internal Server Error', { status: 500 })),
    );

    const res = await GET();
    expect(res.status).toBe(502);
  });
});

describe('POST /api/policy/grants', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockResolvedValue(
      makeSession() as ReturnType<typeof makeSession> as never,
    );
    vi.mocked(isSessionValid).mockReturnValue(true);
  });

  it('returns 401 when the session is invalid', async () => {
    vi.mocked(isSessionValid).mockReturnValue(false);
    const res = await POST(
      makePostRequest({ principal_type: 'HUMAN', principal_id: 'u1', scopes: [] }),
    );
    expect(res.status).toBe(401);
  });

  it('returns 400 when the request body is not valid JSON', async () => {
    const req = new NextRequest('http://localhost/api/policy/grants', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: 'not-json',
    });
    const res = await POST(req);
    expect(res.status).toBe(400);
    const body = (await res.json()) as { code: string };
    expect(body.code).toBe('bad_request');
  });

  it('returns 201 with grant data on success', async () => {
    server.use(http.post(GRANTS_URL, () => HttpResponse.json(GRANT_FIXTURE, { status: 201 })));

    const res = await POST(
      makePostRequest({
        principal_type: 'HUMAN',
        principal_id: 'user-2',
        scopes: ['platform:grants:read'],
      }),
    );
    expect(res.status).toBe(201);
    const body = (await res.json()) as { data: typeof GRANT_FIXTURE };
    expect(body.data.id).toBe('grant-new');
  });

  it('forwards the session tenantId when the body omits tenant_id', async () => {
    let capturedTenantId: string | undefined;
    server.use(
      http.post(GRANTS_URL, async ({ request }) => {
        const b = (await request.json()) as { tenant_id?: string };
        capturedTenantId = b.tenant_id;
        return HttpResponse.json(GRANT_FIXTURE, { status: 201 });
      }),
    );

    await POST(makePostRequest({ principal_type: 'HUMAN', principal_id: 'user-2', scopes: [] }));
    expect(capturedTenantId).toBe('test-tenant');
  });

  it('maps an upstream 5xx to 502', async () => {
    server.use(
      http.post(GRANTS_URL, () => new HttpResponse('Internal Server Error', { status: 500 })),
    );

    const res = await POST(
      makePostRequest({ principal_type: 'HUMAN', principal_id: 'u1', scopes: [] }),
    );
    expect(res.status).toBe(502);
  });
});
