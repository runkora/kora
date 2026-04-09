/**
 * Integration tests for POST /api/auth/session and GET /api/auth/session.
 *
 * Strategy:
 * - `next/headers` and `iron-session` are vi.mock'd — they have no Node
 *   equivalent and would throw in Vitest's jsdom environment.
 * - `@orbit/server/config` is vi.mock'd so we control `orbitApiUrl` and
 *   avoid the `requireEnv` throw at import time.
 * - `@orbit/server/session` is vi.mock'd so GET tests control session state
 *   without a real iron-session cookie.
 * - MSW intercepts the upstream fetch the POST handler makes to the Go
 *   identity service (`http://test-orbit-api/identity/session`).
 * - `getSessionOptions` from `@orbit/server/auth-config` runs for real —
 *   it reads `BFF_SESSION_SECRET` at call time, so we stub that env var
 *   in beforeAll.
 */
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import type { IronSession } from 'iron-session';
import { server } from '@orbit/test-utils';
import {
  createBackendSessionResponse,
  createSessionUser,
  createSessionPayload,
} from '@orbit/test-utils/factories/session';
import type { SessionPayload } from '@orbit/server/auth-config';

// ── Module mocks (hoisted before imports) ─────────────────────────────────────

vi.mock('next/headers', () => ({
  cookies: vi.fn().mockResolvedValue({}),
}));

vi.mock('iron-session', () => ({
  getIronSession: vi.fn(),
}));

vi.mock('@orbit/server/config', () => ({
  config: { orbitApiUrl: 'http://test-orbit-api' },
}));

vi.mock('@orbit/server/session', () => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn(),
}));

// ── Imports that depend on the mocks above ────────────────────────────────────

import { POST, GET } from './route';
import { getIronSession } from 'iron-session';
import { getSession, isSessionValid } from '@orbit/server/session';

// ── Environment setup ─────────────────────────────────────────────────────────

const TEST_SECRET = 'test-bff-secret-that-is-at-least-32-chars!';

beforeAll(() => {
  vi.stubEnv('BFF_SESSION_SECRET', TEST_SECRET);
});

afterAll(() => {
  vi.unstubAllEnvs();
});

// ── Helpers ───────────────────────────────────────────────────────────────────

/** Returns a mock iron-session object with a tracked save() function. */
function makeMockSession() {
  const save = vi.fn().mockResolvedValue(undefined);
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  vi.mocked(getIronSession).mockResolvedValue({ save } as any);
  return { save };
}

/** Builds a POST Request with a JSON body. */
function makePostRequest(body: unknown): Request {
  return new Request('http://localhost/api/auth/session', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

// ── POST /api/auth/session ────────────────────────────────────────────────────

describe('POST /api/auth/session', () => {
  beforeEach(() => {
    vi.mocked(getIronSession).mockReset();
  });

  describe('request validation', () => {
    it('returns 400 when the body is not valid JSON', async () => {
      const req = new Request('http://localhost/api/auth/session', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: 'not-json{{{',
      });
      const res = await POST(req);
      expect(res.status).toBe(400);
      const body = (await res.json()) as Record<string, unknown>;
      expect(body.error).toBe('bad_request');
    });

    it('returns 400 when id_token is missing', async () => {
      const req = makePostRequest({ tenant_id: 'tenant-1' });
      const res = await POST(req);
      expect(res.status).toBe(400);
      const body = (await res.json()) as Record<string, unknown>;
      expect(body.error).toBe('bad_request');
    });

    it('returns 400 when id_token is an empty string', async () => {
      const req = makePostRequest({ id_token: '', tenant_id: 'tenant-1' });
      const res = await POST(req);
      expect(res.status).toBe(400);
      const body = (await res.json()) as Record<string, unknown>;
      expect(body.error).toBe('bad_request');
    });

    it('returns 400 when tenant_id is missing', async () => {
      const req = makePostRequest({ id_token: 'some-token' });
      const res = await POST(req);
      expect(res.status).toBe(400);
      const body = (await res.json()) as Record<string, unknown>;
      expect(body.error).toBe('bad_request');
    });

    it('returns 400 when tenant_id is an empty string', async () => {
      const req = makePostRequest({ id_token: 'some-token', tenant_id: '' });
      const res = await POST(req);
      expect(res.status).toBe(400);
      const body = (await res.json()) as Record<string, unknown>;
      expect(body.error).toBe('bad_request');
    });
  });

  describe('upstream error handling', () => {
    it('returns 502 when the upstream is unreachable', async () => {
      server.use(
        http.post('http://test-orbit-api/identity/session', () => {
          throw new Error('Network error');
        }),
      );

      const req = makePostRequest({ id_token: 'tok', tenant_id: 'tenant-1' });
      const res = await POST(req);
      expect(res.status).toBe(502);
      const body = (await res.json()) as Record<string, unknown>;
      expect(body.error).toBe('upstream_error');
    });

    it('returns 401 when the upstream returns 401', async () => {
      server.use(
        http.post('http://test-orbit-api/identity/session', () =>
          HttpResponse.json({ error: 'unauthorized' }, { status: 401 }),
        ),
      );

      const req = makePostRequest({ id_token: 'tok', tenant_id: 'tenant-1' });
      const res = await POST(req);
      expect(res.status).toBe(401);
      const body = (await res.json()) as Record<string, unknown>;
      expect(body.error).toBe('unauthorized');
    });

    it('returns 400 when the upstream returns a 4xx error', async () => {
      server.use(
        http.post('http://test-orbit-api/identity/session', () =>
          HttpResponse.json({ error: 'invalid_grant' }, { status: 422 }),
        ),
      );

      const req = makePostRequest({ id_token: 'tok', tenant_id: 'tenant-1' });
      const res = await POST(req);
      expect(res.status).toBe(400);
      const body = (await res.json()) as Record<string, unknown>;
      expect(body.error).toBe('bad_request');
    });

    it('returns 502 when the upstream returns a 5xx error', async () => {
      server.use(
        http.post('http://test-orbit-api/identity/session', () =>
          HttpResponse.json({ error: 'internal' }, { status: 500 }),
        ),
      );

      const req = makePostRequest({ id_token: 'tok', tenant_id: 'tenant-1' });
      const res = await POST(req);
      expect(res.status).toBe(502);
      const body = (await res.json()) as Record<string, unknown>;
      expect(body.error).toBe('upstream_error');
    });

    it('returns 502 when the upstream returns 200 with an invalid JSON body', async () => {
      // Exercises the try/catch around `backendRes.json()`. A backend that
      // returns a non-JSON body with a 200 status should never happen in
      // production, but a malformed response must not cause an unhandled
      // exception — the route must return a controlled 502.
      server.use(
        http.post(
          'http://test-orbit-api/identity/session',
          () =>
            new HttpResponse('not-json', {
              status: 200,
              headers: { 'Content-Type': 'application/json' },
            }),
        ),
      );

      const req = makePostRequest({ id_token: 'tok', tenant_id: 'tenant-1' });
      const res = await POST(req);
      expect(res.status).toBe(502);
      const body = (await res.json()) as Record<string, unknown>;
      expect(body.error).toBe('upstream_error');
    });

    it('returns 502 when the upstream returns 200 with a malformed JWT', async () => {
      // Exercises the try/catch around `decodeJwtPayload`. A backend regression
      // that returns a structurally invalid token (fewer than 3 dot-separated
      // segments) must produce a 502 rather than an unhandled exception. This
      // is the only code path where decoding fails after a successful HTTP exchange.
      server.use(
        http.post('http://test-orbit-api/identity/session', () =>
          HttpResponse.json(createBackendSessionResponse({ access_token: 'only.two-segments' })),
        ),
      );

      const req = makePostRequest({ id_token: 'tok', tenant_id: 'tenant-1' });
      const res = await POST(req);
      expect(res.status).toBe(502);
      const body = (await res.json()) as Record<string, unknown>;
      expect(body.error).toBe('upstream_error');
    });
  });

  describe('success path', () => {
    it('returns 200 with an empty body on success', async () => {
      const backendData = createBackendSessionResponse();
      server.use(
        http.post('http://test-orbit-api/identity/session', () => HttpResponse.json(backendData)),
      );
      makeMockSession();

      const req = makePostRequest({ id_token: backendData.access_token, tenant_id: 'tenant-1' });
      const res = await POST(req);

      expect(res.status).toBe(200);
      expect(await res.json()).toEqual({});
    });

    it('saves the session cookie on success', async () => {
      const backendData = createBackendSessionResponse();
      server.use(
        http.post('http://test-orbit-api/identity/session', () => HttpResponse.json(backendData)),
      );
      const { save } = makeMockSession();

      const req = makePostRequest({ id_token: backendData.access_token, tenant_id: 'tenant-1' });
      await POST(req);

      expect(save).toHaveBeenCalledOnce();
    });

    it('stores correct user identity in the session', async () => {
      // Verifies the full SessionPayload shape written to the cookie — all five
      // fields. Uses the factory's user override so JWT construction stays in
      // one place (the factory) rather than being duplicated inline.
      const user = createSessionUser({
        sub: 'user-alice-123',
        email: 'alice@orbit.local',
        name: 'Alice',
      });
      const backendData = createBackendSessionResponse({}, user);

      server.use(
        http.post('http://test-orbit-api/identity/session', () => HttpResponse.json(backendData)),
      );

      const save = vi.fn().mockResolvedValue(undefined);
      let capturedSession: Record<string, unknown> = {};
      vi.mocked(getIronSession).mockImplementation(async () => {
        const obj: Record<string, unknown> = { save };
        // Capture what Object.assign writes into the session
        return new Proxy(obj, {
          set(target, key, value) {
            target[key as string] = value;
            capturedSession = { ...target };
            return true;
          },
        }) as unknown as IronSession<SessionPayload>;
      });

      const req = makePostRequest({ id_token: backendData.access_token, tenant_id: 'tenant-1' });
      await POST(req);

      expect(capturedSession.user).toEqual({ sub: user.sub, email: user.email, name: user.name });
      expect(capturedSession.tenantId).toBe('tenant-1');
      expect(typeof capturedSession.expiresAt).toBe('number');
    });
  });
});

// ── GET /api/auth/session ─────────────────────────────────────────────────────

describe('GET /api/auth/session', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockReset();
    vi.mocked(isSessionValid).mockReset();
  });

  it('returns 401 when there is no active session', async () => {
    vi.mocked(getSession).mockResolvedValue({} as unknown as IronSession<SessionPayload>);
    vi.mocked(isSessionValid).mockReturnValue(false);

    const res = await GET();

    expect(res.status).toBe(401);
    const body = (await res.json()) as Record<string, unknown>;
    expect(body.error).toBe('unauthorized');
  });

  it('returns 200 with user and expiresAt when the session is valid', async () => {
    const payload = createSessionPayload();
    vi.mocked(getSession).mockResolvedValue(payload as unknown as IronSession<SessionPayload>);
    vi.mocked(isSessionValid).mockReturnValue(true);

    const res = await GET();

    expect(res.status).toBe(200);
    const body = (await res.json()) as Record<string, unknown>;
    expect(body.user).toEqual(payload.user);
    expect(body.expiresAt).toBe(payload.expiresAt);
  });

  it('does not expose accessToken, refreshToken, or tenantId in the response', async () => {
    // Security assertion: tokens and tenant routing info must never leak to
    // the browser. They are sealed inside the HttpOnly iron-session cookie only.
    const payload = createSessionPayload();
    vi.mocked(getSession).mockResolvedValue(payload as unknown as IronSession<SessionPayload>);
    vi.mocked(isSessionValid).mockReturnValue(true);

    const res = await GET();
    const body = (await res.json()) as Record<string, unknown>;

    expect(body).not.toHaveProperty('accessToken');
    expect(body).not.toHaveProperty('refreshToken');
    expect(body).not.toHaveProperty('tenantId');
  });
});
