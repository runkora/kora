import { describe, it, expect, vi, beforeEach } from 'vitest';
import { NextRequest, NextResponse } from 'next/server';
import { withAuth } from './route-helpers';

// ── Module mocks ──────────────────────────────────────────────────────────────

vi.mock('@runkora/server/session', () => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn(),
}));

import { getSession, isSessionValid } from '@runkora/server/session';

// ── Helpers ───────────────────────────────────────────────────────────────────

function makeSession(overrides: Record<string, unknown> = {}) {
  return {
    user: { sub: 'test-sub', email: 'test@orbit.local', name: 'Test' },
    accessToken: 'test-access-token',
    refreshToken: 'test-refresh',
    expiresAt: Math.floor(Date.now() / 1000) + 3600,
    tenantId: 'test-tenant',
    permissions: { scopes: [], evaluatedAt: 0 },
    ...overrides,
  };
}

function makeRequest(): NextRequest {
  return new NextRequest('http://localhost/api/test');
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('withAuth', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockResolvedValue(
      makeSession() as ReturnType<typeof makeSession> as never,
    );
    vi.mocked(isSessionValid).mockReturnValue(true);
  });

  it('returns 401 when isSessionValid returns false', async () => {
    vi.mocked(isSessionValid).mockReturnValue(false);
    const handler = withAuth(vi.fn().mockResolvedValue(NextResponse.json({ ok: true })));
    const res = await handler(makeRequest());
    expect(res.status).toBe(401);
    const body = (await res.json()) as { code: string };
    expect(body.code).toBe('unauthorized');
  });

  it('calls the inner handler and returns its response when session is valid', async () => {
    const inner = vi.fn().mockResolvedValue(NextResponse.json({ data: 'ok' }, { status: 200 }));
    const handler = withAuth(inner);
    const res = await handler(makeRequest());
    expect(res.status).toBe(200);
    expect(inner).toHaveBeenCalledOnce();
  });

  it('passes the validated session as the second argument to the inner handler', async () => {
    const session = makeSession();
    vi.mocked(getSession).mockResolvedValue(session as never);
    let capturedSession: unknown;
    const inner = vi.fn().mockImplementation((_req, sess) => {
      capturedSession = sess;
      return Promise.resolve(NextResponse.json({ ok: true }));
    });
    await withAuth(inner)(makeRequest());
    expect(capturedSession).toBe(session);
  });

  it('passes the original request as the first argument to the inner handler', async () => {
    const req = makeRequest();
    let capturedReq: unknown;
    const inner = vi.fn().mockImplementation((r) => {
      capturedReq = r;
      return Promise.resolve(NextResponse.json({ ok: true }));
    });
    await withAuth(inner)(req);
    expect(capturedReq).toBe(req);
  });

  it('does not call the inner handler when the session is invalid', async () => {
    vi.mocked(isSessionValid).mockReturnValue(false);
    const inner = vi.fn();
    await withAuth(inner)(makeRequest());
    expect(inner).not.toHaveBeenCalled();
  });
});
