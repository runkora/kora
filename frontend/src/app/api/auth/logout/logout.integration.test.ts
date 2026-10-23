/**
 * Integration tests for POST /api/auth/logout
 *
 * Strategy:
 * - `@runkora/server/config` is vi.mock'd so `config.koraApiUrl` resolves to
 *   `http://test-kora-api` without triggering `requireEnv` at import time.
 * - `@runkora/server/session` is vi.mock'd so tests control the session state
 *   (logged-in or logged-out) without a real iron-session cookie.
 * - MSW intercepts calls to `http://test-kora-api/identity/revoke` so
 *   revocation behaviour (success, failure, network error) can be exercised.
 * - `server-only` is shimmed by the Vitest alias so the bare import at the
 *   top of the route module does not throw in Node/jsdom.
 *
 * Key invariant (RFC 7009 §2.2): even when revocation fails, the local
 * session cookie must be destroyed and the browser must be redirected to
 * /login.  Every test therefore asserts `destroy` was called.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { NextRequest } from 'next/server';
import { server } from '@runkora/test-utils';
import { createSessionPayload } from '@runkora/test-utils/factories/session';

// ── Module mocks ──────────────────────────────────────────────────────────────

vi.mock('next/headers', () => ({
  cookies: vi.fn().mockResolvedValue({}),
}));

vi.mock('@runkora/server/config', () => ({
  config: { koraApiUrl: 'http://test-kora-api' },
}));

vi.mock('@runkora/server/session', () => ({
  getSession: vi.fn(),
}));

// ── Import under test ─────────────────────────────────────────────────────────

import { POST } from './route';
import { getSession } from '@runkora/server/session';

// ── Helpers ───────────────────────────────────────────────────────────────────

const REVOKE_URL = 'http://test-kora-api/identity/revoke';

function makePostRequest(): NextRequest {
  return new NextRequest('http://localhost/api/auth/logout', { method: 'POST' });
}

/**
 * Returns a mock session with the given payload written to it.
 * `destroy` is a tracked spy so tests can assert it was called.
 */
function makeMockSession(payload: ReturnType<typeof createSessionPayload> | null) {
  const destroy = vi.fn().mockResolvedValue(undefined);
  const session = {
    ...(payload ?? {}),
    user: payload?.user ?? undefined,
    accessToken: payload?.accessToken ?? '',
    refreshToken: payload?.refreshToken ?? '',
    destroy,
  };
  vi.mocked(getSession).mockResolvedValue(
    session as unknown as Awaited<ReturnType<typeof getSession>>,
  );
  return { destroy };
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('POST /api/auth/logout', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockReset();

    // Default: revocation succeeds — individual tests override as needed.
    server.use(http.post(REVOKE_URL, () => new HttpResponse(null, { status: 200 })));
  });

  describe('when the user is logged in', () => {
    it('destroys the session cookie and redirects to /login', async () => {
      const { destroy } = makeMockSession(createSessionPayload());

      const res = await POST(makePostRequest());

      expect(destroy).toHaveBeenCalledOnce();
      expect(res.status).toBe(302);
      expect(res.headers.get('location')).toMatch(/\/login$/);
    });

    it('sends revocation requests for both access_token and refresh_token', async () => {
      const revokedHints: string[] = [];
      server.use(
        http.post(REVOKE_URL, async ({ request }) => {
          const text = await request.text();
          const params = new URLSearchParams(text);
          const hint = params.get('token_type_hint');
          if (hint) revokedHints.push(hint);
          return new HttpResponse(null, { status: 200 });
        }),
      );

      makeMockSession(createSessionPayload());
      await POST(makePostRequest());

      expect(revokedHints).toHaveLength(2);
      expect(revokedHints).toContain('access_token');
      expect(revokedHints).toContain('refresh_token');
    });

    it('still destroys the session and redirects when revocation returns an error status', async () => {
      server.use(http.post(REVOKE_URL, () => new HttpResponse(null, { status: 500 })));

      const { destroy } = makeMockSession(createSessionPayload());
      const res = await POST(makePostRequest());

      expect(destroy).toHaveBeenCalledOnce();
      expect(res.status).toBe(302);
      expect(res.headers.get('location')).toMatch(/\/login$/);
    });

    it('still destroys the session and redirects when the revocation endpoint is unreachable', async () => {
      server.use(http.post(REVOKE_URL, () => HttpResponse.error()));

      const { destroy } = makeMockSession(createSessionPayload());
      const res = await POST(makePostRequest());

      expect(destroy).toHaveBeenCalledOnce();
      expect(res.status).toBe(302);
      expect(res.headers.get('location')).toMatch(/\/login$/);
    });
  });

  describe('when the user is already logged out (no session.user)', () => {
    it('skips revocation, destroys the session, and redirects to /login', async () => {
      const revokeCallCount = { n: 0 };
      server.use(
        http.post(REVOKE_URL, () => {
          revokeCallCount.n += 1;
          return new HttpResponse(null, { status: 200 });
        }),
      );

      const { destroy } = makeMockSession(null);
      const res = await POST(makePostRequest());

      expect(revokeCallCount.n).toBe(0);
      expect(destroy).toHaveBeenCalledOnce();
      expect(res.status).toBe(302);
      expect(res.headers.get('location')).toMatch(/\/login$/);
    });
  });
});
