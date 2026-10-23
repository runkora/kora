/**
 * Integration tests for GET /api/policy/roles and PUT /api/policy/roles
 *
 * Strategy:
 * - `@runkora/server/config` is vi.mock'd to set `policyServiceUrl`.
 * - `@runkora/server/session` is vi.mock'd to control session state.
 * - MSW intercepts calls to `http://test-policy-svc/policy/roles`.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { NextRequest } from 'next/server';
import { server } from '@runkora/test-utils';

// ── Module mocks ──────────────────────────────────────────────────────────────

vi.mock('@runkora/server/config', () => ({
  config: { policyServiceUrl: 'http://test-policy-svc' },
}));

vi.mock('@runkora/server/session', () => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn().mockReturnValue(true),
}));

// ── Import under test ─────────────────────────────────────────────────────────

import { GET, PUT } from './route';
import { getSession, isSessionValid } from '@runkora/server/session';

// ── Helpers ───────────────────────────────────────────────────────────────────

const ROLES_URL = 'http://test-policy-svc/policy/roles';

function makeSession(overrides: Record<string, unknown> = {}) {
  return {
    user: { sub: 'test-sub', email: 'test@runkora.test', name: 'Test' },
    accessToken: 'test-access-token',
    refreshToken: 'test-refresh',
    expiresAt: Math.floor(Date.now() / 1000) + 3600,
    tenantId: 'test-tenant',
    permissions: { scopes: ['platform:grants:read'], evaluatedAt: 0 },
    ...overrides,
  };
}

function makePutRequest(body: unknown): NextRequest {
  return new NextRequest('http://localhost/api/policy/roles', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

const ROLES_FIXTURE = {
  roles: [
    {
      id: 'role-1',
      tenant_id: 'test-tenant',
      role: 'admin',
      scopes: ['platform:grants:read', 'platform:grants:write'],
      created_at: '2026-01-01T00:00:00Z',
    },
  ],
};

const ROLE_FIXTURE = {
  id: 'role-new',
  tenant_id: 'test-tenant',
  role: 'viewer',
  scopes: ['platform:grants:read'],
  created_at: '2026-01-01T00:00:00Z',
};

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('GET /api/policy/roles', () => {
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

  it('returns 200 with roles data on success', async () => {
    server.use(http.get(ROLES_URL, () => HttpResponse.json(ROLES_FIXTURE)));

    const res = await GET();
    expect(res.status).toBe(200);
    const body = (await res.json()) as { data: typeof ROLES_FIXTURE };
    expect(body.data).toEqual(ROLES_FIXTURE);
  });

  it('maps an upstream 5xx to 502', async () => {
    server.use(
      http.get(ROLES_URL, () => new HttpResponse('Internal Server Error', { status: 500 })),
    );

    const res = await GET();
    expect(res.status).toBe(502);
  });
});

describe('PUT /api/policy/roles', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockResolvedValue(
      makeSession() as ReturnType<typeof makeSession> as never,
    );
    vi.mocked(isSessionValid).mockReturnValue(true);
  });

  it('returns 401 when the session is invalid', async () => {
    vi.mocked(isSessionValid).mockReturnValue(false);
    const res = await PUT(makePutRequest({ role: 'viewer', scopes: [] }));
    expect(res.status).toBe(401);
  });

  it('returns 400 when the request body is not valid JSON', async () => {
    const req = new NextRequest('http://localhost/api/policy/roles', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: 'not-json',
    });
    const res = await PUT(req);
    expect(res.status).toBe(400);
    const body = (await res.json()) as { code: string };
    expect(body.code).toBe('bad_request');
  });

  it('returns 200 with role data on success', async () => {
    server.use(http.put(ROLES_URL, () => HttpResponse.json(ROLE_FIXTURE)));

    const res = await PUT(makePutRequest({ role: 'viewer', scopes: ['platform:grants:read'] }));
    expect(res.status).toBe(200);
    const body = (await res.json()) as { data: typeof ROLE_FIXTURE };
    expect(body.data.role).toBe('viewer');
  });

  it('forwards the session tenantId when the body omits tenant_id', async () => {
    let capturedTenantId: string | undefined;
    server.use(
      http.put(ROLES_URL, async ({ request }) => {
        const b = (await request.json()) as { tenant_id?: string };
        capturedTenantId = b.tenant_id;
        return HttpResponse.json(ROLE_FIXTURE);
      }),
    );

    await PUT(makePutRequest({ role: 'viewer', scopes: [] }));
    expect(capturedTenantId).toBe('test-tenant');
  });

  it('maps an upstream 5xx to 502', async () => {
    server.use(
      http.put(ROLES_URL, () => new HttpResponse('Internal Server Error', { status: 500 })),
    );

    const res = await PUT(makePutRequest({ role: 'viewer', scopes: [] }));
    expect(res.status).toBe(502);
  });
});
