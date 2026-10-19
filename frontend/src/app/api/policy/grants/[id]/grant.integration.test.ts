/**
 * Integration tests for DELETE /api/policy/grants/[id]
 *
 * Strategy:
 * - `@runkora/server/config` is vi.mock'd to set `policyServiceUrl`.
 * - `@runkora/server/session` is vi.mock'd to control session state.
 * - MSW intercepts DELETE calls to `http://test-policy-svc/policy/grants/:id`.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
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

import { DELETE } from './route';
import { getSession, isSessionValid } from '@runkora/server/session';

// ── Helpers ───────────────────────────────────────────────────────────────────

const GRANTS_BASE_URL = 'http://test-policy-svc/policy/grants';

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

function makeDeleteArgs(id: string) {
  return [
    new Request(`http://localhost/api/policy/grants/${id}`, { method: 'DELETE' }),
    { params: Promise.resolve({ id }) },
  ] as const;
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('DELETE /api/policy/grants/[id]', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockResolvedValue(
      makeSession() as ReturnType<typeof makeSession> as never,
    );
    vi.mocked(isSessionValid).mockReturnValue(true);
  });

  it('returns 401 when the session is invalid', async () => {
    vi.mocked(isSessionValid).mockReturnValue(false);
    const res = await DELETE(...makeDeleteArgs('grant-1'));
    expect(res.status).toBe(401);
  });

  it('returns 204 (no body) on success', async () => {
    server.use(
      http.delete(`${GRANTS_BASE_URL}/:id`, () => new HttpResponse(null, { status: 204 })),
    );

    const res = await DELETE(...makeDeleteArgs('grant-1'));
    expect(res.status).toBe(204);
    expect(await res.text()).toBe('');
  });

  it('maps an upstream 404 to 404', async () => {
    server.use(
      http.delete(`${GRANTS_BASE_URL}/:id`, () => new HttpResponse('not found', { status: 404 })),
    );

    const res = await DELETE(...makeDeleteArgs('nonexistent'));
    expect(res.status).toBe(404);
  });

  it('maps an upstream 5xx to 502', async () => {
    server.use(
      http.delete(`${GRANTS_BASE_URL}/:id`, () => new HttpResponse('error', { status: 500 })),
    );

    const res = await DELETE(...makeDeleteArgs('grant-1'));
    expect(res.status).toBe(502);
  });
});
