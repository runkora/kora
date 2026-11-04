/**
 * Integration tests for POST /api/auth/session and GET /api/auth/session.
 *
 * Strategy:
 * - `next/headers` and `iron-session` are vi.mock'd — they have no Node
 *   equivalent and would throw in Vitest's jsdom environment.
 * - `@runkora/server/config` is vi.mock'd so we control `koraApiUrl` and
 *   avoid the `requireEnv` throw at import time.
 * - `@runkora/server/session` is vi.mock'd so GET tests control session state
 *   without a real iron-session cookie.
 * - MSW intercepts the upstream fetch the POST handler makes to the Go
 *   identity service (`http://test-kora-api/identity/session`).
 * - `getSessionOptions` from `@runkora/server/auth-config` runs for real —
 *   it reads `BFF_SESSION_SECRET` at call time, so we stub that env var
 *   in beforeAll.
 */
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import type { IronSession, SessionOptions } from 'iron-session';
import { server } from '@runkora/test-utils';
import {
  createBackendSessionResponse,
  createSessionUser,
  createSessionPayload,
} from '@runkora/test-utils/factories/session';
import type { SessionPayload } from '@runkora/server/auth-config';

// ── Module mocks (hoisted before imports) ─────────────────────────────────────

const cookieJar = vi.hoisted(() => ({ get: vi.fn(), set: vi.fn() }));
vi.mock('next/headers', () => ({
  cookies: vi.fn().mockResolvedValue(cookieJar),
}));

vi.mock('iron-session', () => ({
  getIronSession: vi.fn(),
}));

vi.mock('@runkora/server/config', () => ({
  config: { koraApiUrl: 'http://test-kora-api', policyGatewayUrl: 'http://test-policy-gateway' },
}));

vi.mock('@runkora/server/session', () => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn(),
}));

// ── Imports that depend on the mocks above ────────────────────────────────────

import { POST, GET } from './route';
import { getIronSession } from 'iron-session';
import { getSession, isSessionValid } from '@runkora/server/session';

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
  vi.mocked(getIronSession).mockResolvedValue({ save } as unknown as IronSession<SessionPayload>);
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
    vi.mocked(getSession).mockReset();
    cookieJar.get.mockReset();
    cookieJar.set.mockReset();
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
        http.post('http://test-kora-api/identity/session', () => {
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
        http.post('http://test-kora-api/identity/session', () =>
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
        http.post('http://test-kora-api/identity/session', () =>
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
        http.post('http://test-kora-api/identity/session', () =>
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
          'http://test-kora-api/identity/session',
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
        http.post('http://test-kora-api/identity/session', () =>
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
        http.post('http://test-kora-api/identity/session', () => HttpResponse.json(backendData)),
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
        http.post('http://test-kora-api/identity/session', () => HttpResponse.json(backendData)),
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
        email: 'alice@runkora.test',
        name: 'Alice',
      });
      const backendData = createBackendSessionResponse({}, user);

      server.use(
        http.post('http://test-kora-api/identity/session', () => HttpResponse.json(backendData)),
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
      expect(capturedSession.refreshExpiresAt).toBe(
        backendData.issued_at + (backendData.refresh_token_expires_in ?? 0),
      );
    });

    it('stores the tenant from the issued token, not the request', async () => {
      const backendData = createBackendSessionResponse({}, createSessionUser(), 'kora');
      server.use(
        http.post('http://test-kora-api/identity/session', () => HttpResponse.json(backendData)),
      );
      const captured: Record<string, unknown> = { save: vi.fn().mockResolvedValue(undefined) };
      vi.mocked(getIronSession).mockResolvedValue(
        captured as unknown as IronSession<SessionPayload>,
      );

      await POST(makePostRequest({ id_token: 'tok', tenant_id: 'constellation' }));

      expect(captured.tenantId).toBe('kora');
    });

    it.each([
      [false, false],
      [true, true],
      [undefined, true],
    ])('stores api_authoring %s as apiAuthoring %s', async (upstream, stored) => {
      const backendData = { ...createBackendSessionResponse(), api_authoring: upstream };
      server.use(
        http.post('http://test-kora-api/identity/session', () => HttpResponse.json(backendData)),
      );
      const session: Record<string, unknown> = { save: vi.fn().mockResolvedValue(undefined) };
      vi.mocked(getIronSession).mockResolvedValue(
        session as unknown as IronSession<SessionPayload>,
      );

      await POST(makePostRequest({ id_token: backendData.access_token, tenant_id: 'tenant-1' }));

      expect(session.apiAuthoring).toBe(stored);
    });
  });

  describe('tenant switching', () => {
    const TENANTS = [
      { id: 'kora-tenant-example', display_name: 'Example Platform' },
      { id: 'kora-tenant-example-data', display_name: 'Example Data' },
    ];

    /** Captures the body the route sends to identity and answers with `reply`. */
    function upstream(reply: () => Response) {
      const sent: Record<string, unknown>[] = [];
      server.use(
        http.post('http://test-kora-api/identity/session', async ({ request }) => {
          sent.push((await request.json()) as Record<string, unknown>);
          return reply();
        }),
      );
      return sent;
    }

    function captureSession() {
      const captured: Record<string, unknown> = { save: vi.fn().mockResolvedValue(undefined) };
      vi.mocked(getIronSession).mockResolvedValue(
        captured as unknown as IronSession<SessionPayload>,
      );
      return captured;
    }

    function currentSession(overrides: Partial<SessionPayload> = {}) {
      const s = {
        ...createSessionPayload({ tenantId: 'kora-tenant-example', refreshToken: 'rt-old' }),
        ...overrides,
        save: vi.fn().mockResolvedValue(undefined),
      };
      vi.mocked(getSession).mockResolvedValue(s as unknown as IronSession<SessionPayload>);
      return s;
    }

    it.each([
      ['the cookie', 'kora-tenant-example-data', undefined, 'kora-tenant-example-data'],
      ['no hint', undefined, undefined, undefined],
      [
        'the pick over the cookie',
        'kora-tenant-example',
        'kora-tenant-example-data',
        'kora-tenant-example-data',
      ],
    ])('sends %s as the hint', async (_, cookie, pick, expected) => {
      cookieJar.get.mockReturnValue(cookie ? { value: cookie } : undefined);
      const sent = upstream(() => HttpResponse.json(createBackendSessionResponse()));
      captureSession();

      await POST(
        makePostRequest({ id_token: 'tok', tenant_id: 'kora-tenant-example', tenant_hint: pick }),
      );

      expect(sent[0]?.tenant_id).toBe('kora-tenant-example');
      expect(sent[0]?.tenant_hint).toBe(expected);
      expect(sent[0]).not.toHaveProperty('replaces_refresh_token');
    });

    it('seals the eligible set and sets the last-used cookie to the issued tenant', async () => {
      upstream(() =>
        HttpResponse.json({
          ...createBackendSessionResponse({}, createSessionUser(), 'kora-tenant-example-data'),
          tenants: TENANTS,
        }),
      );
      const captured = captureSession();

      const res = await POST(
        makePostRequest({ id_token: 'tok', tenant_id: 'kora-tenant-example' }),
      );

      expect(res.status).toBe(200);
      expect(captured.tenants).toEqual([
        { id: 'kora-tenant-example', displayName: 'Example Platform' },
        { id: 'kora-tenant-example-data', displayName: 'Example Data' },
      ]);
      expect(cookieJar.set).toHaveBeenCalledWith(
        'kora_last_tenant',
        'kora-tenant-example-data',
        expect.objectContaining({ httpOnly: true, sameSite: 'lax', path: '/' }),
      );
    });

    it('passes 409 matches to the callback', async () => {
      upstream(() =>
        HttpResponse.json(
          {
            code: 'multiple_tenants_match',
            message: 'the account matches several tenants',
            details: { tenants: TENANTS },
          },
          { status: 409 },
        ),
      );

      const res = await POST(
        makePostRequest({ id_token: 'tok', tenant_id: 'kora-tenant-example' }),
      );

      expect(res.status).toBe(409);
      expect(await res.json()).toEqual({
        error: 'multiple_tenants_match',
        tenants: [
          { id: 'kora-tenant-example', displayName: 'Example Platform' },
          { id: 'kora-tenant-example-data', displayName: 'Example Data' },
        ],
      });
      expect(cookieJar.set).not.toHaveBeenCalled();
    });

    it('requires a target for a switch', async () => {
      const res = await POST(
        makePostRequest({ id_token: 'tok', tenant_id: 'kora-tenant-example', switch: true }),
      );
      expect(res.status).toBe(400);
    });

    it('switches with the session tenant and refresh token, then swaps the cookie', async () => {
      currentSession();
      const sent = upstream(() =>
        HttpResponse.json({
          ...createBackendSessionResponse({}, createSessionUser(), 'kora-tenant-example-data'),
          tenants: TENANTS,
        }),
      );
      const captured = captureSession();

      const res = await POST(
        makePostRequest({
          id_token: 'tok',
          tenant_id: 'discovered-tenant',
          tenant_hint: 'kora-tenant-example-data',
          switch: true,
        }),
      );

      expect(res.status).toBe(200);
      expect(sent[0]).toMatchObject({
        tenant_id: 'kora-tenant-example',
        tenant_hint: 'kora-tenant-example-data',
        replaces_refresh_token: 'rt-old',
      });
      expect(captured.tenantId).toBe('kora-tenant-example-data');
      expect(cookieJar.set).toHaveBeenCalledWith(
        'kora_last_tenant',
        'kora-tenant-example-data',
        expect.anything(),
      );
    });

    it('drops the eligible set when the cookie would be too big', async () => {
      upstream(() => HttpResponse.json({ ...createBackendSessionResponse(), tenants: TENANTS }));
      const save = vi
        .fn()
        .mockRejectedValueOnce(new Error('iron-session: Cookie length is too big'))
        .mockResolvedValue(undefined);
      const captured: Record<string, unknown> = { save };
      vi.mocked(getIronSession).mockResolvedValue(
        captured as unknown as IronSession<SessionPayload>,
      );

      const res = await POST(
        makePostRequest({ id_token: 'tok', tenant_id: 'kora-tenant-example' }),
      );

      expect(res.status).toBe(200);
      expect(save).toHaveBeenCalledTimes(2);
      expect(captured).not.toHaveProperty('tenants');
    });

    it('rejects a cross-site post', async () => {
      const req = new Request('http://localhost/api/auth/session', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Sec-Fetch-Site': 'cross-site' },
        body: JSON.stringify({ id_token: 'tok', tenant_id: 'kora-tenant-example' }),
      });
      const res = await POST(req);
      expect(res.status).toBe(403);
      expect(await res.json()).toEqual({ error: 'forbidden' });
    });

    it.each(['tenant_switch_denied', 'multiple_tenants_match', 'forbidden'])(
      'keeps the session and stores the fresh set on %s during a switch',
      async (code) => {
        const current = currentSession({
          refreshExpiresAt: Math.floor(Date.now() / 1000) + 1000,
          tenants: [
            { id: 'kora-tenant-example', displayName: 'Example Platform' },
            { id: 'kora-tenant-example-data', displayName: 'Example Data' },
          ],
        });
        // The re-read cookie holds the same session as the one being replaced.
        const kept = { ...current, save: vi.fn().mockResolvedValue(undefined) };
        vi.mocked(getIronSession).mockResolvedValue(kept as unknown as IronSession<SessionPayload>);
        upstream(() =>
          HttpResponse.json(
            { code, message: 'denied', details: { tenants: [TENANTS[0]] } },
            { status: code === 'multiple_tenants_match' ? 409 : 403 },
          ),
        );

        const res = await POST(
          makePostRequest({
            id_token: 'tok',
            tenant_id: 'kora-tenant-example',
            tenant_hint: 'kora-tenant-example-data',
            switch: true,
          }),
        );

        expect(res.status).toBe(403);
        expect(await res.json()).toEqual({ error: 'tenant_switch_denied' });
        expect(kept.tenants).toEqual([
          { id: 'kora-tenant-example', displayName: 'Example Platform' },
        ]);
        expect(kept.save).toHaveBeenCalledOnce();
        expect(kept.refreshToken).toBe('rt-old');
        // The save keeps the cookie lifetime sign-in set, not the 30-day default.
        const options = vi.mocked(getIronSession).mock.calls[0]?.[1] as unknown as SessionOptions;
        const maxAge = options.cookieOptions?.maxAge;
        expect(maxAge).toBeGreaterThan(990);
        expect(maxAge).toBeLessThanOrEqual(1000);
        expect(cookieJar.set).not.toHaveBeenCalled();
      },
    );

    it('runs a switch without a session as a sign-in with the target as hint', async () => {
      vi.mocked(getSession).mockResolvedValue({} as unknown as IronSession<SessionPayload>);
      const sent = upstream(() => HttpResponse.json(createBackendSessionResponse()));
      captureSession();

      await POST(
        makePostRequest({
          id_token: 'tok',
          tenant_id: 'kora-tenant-example',
          tenant_hint: 'kora-tenant-example-data',
          switch: true,
        }),
      );

      expect(sent[0]).toMatchObject({
        tenant_id: 'kora-tenant-example',
        tenant_hint: 'kora-tenant-example-data',
      });
      expect(sent[0]).not.toHaveProperty('replaces_refresh_token');
    });
  });
});

// ── GET /api/auth/session ─────────────────────────────────────────────────────

describe('GET /api/auth/session', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockReset();
    vi.mocked(isSessionValid).mockReset();
    // Default: policy gateway returns an empty scope set.
    server.use(
      http.get('http://test-policy-gateway/policy/scopes', () =>
        HttpResponse.json({
          scopes: [],
          tenant_id: 'test-tenant',
          evaluated_at: new Date().toISOString(),
        }),
      ),
    );
  });

  it('returns 401 when there is no active session', async () => {
    vi.mocked(getSession).mockResolvedValue({} as unknown as IronSession<SessionPayload>);
    vi.mocked(isSessionValid).mockReturnValue(false);

    const res = await GET();

    expect(res.status).toBe(401);
    const body = (await res.json()) as Record<string, unknown>;
    expect(body.error).toBe('unauthorized');
  });

  it('returns 200 with user, expiresAt, and permissions when the session is valid', async () => {
    const payload = createSessionPayload();
    vi.mocked(getSession).mockResolvedValue(payload as unknown as IronSession<SessionPayload>);
    vi.mocked(isSessionValid).mockReturnValue(true);
    server.use(
      http.get('http://test-policy-gateway/policy/scopes', () =>
        HttpResponse.json({
          scopes: ['agents:read', 'connections:read'],
          tenant_id: 'test-tenant',
          evaluated_at: new Date().toISOString(),
        }),
      ),
    );

    const res = await GET();

    expect(res.status).toBe(200);
    const body = (await res.json()) as Record<string, unknown>;
    expect(body.user).toEqual(payload.user);
    expect(body.expiresAt).toBe(payload.expiresAt);
    expect((body.permissions as { scopes: string[] }).scopes).toEqual([
      'agents:read',
      'connections:read',
    ]);
  });

  it.each([
    [false, false],
    [undefined, true],
  ])('returns apiAuthoring %s from the session as %s', async (sealed, returned) => {
    const payload = { ...createSessionPayload(), apiAuthoring: sealed };
    vi.mocked(getSession).mockResolvedValue(payload as unknown as IronSession<SessionPayload>);
    vi.mocked(isSessionValid).mockReturnValue(true);

    const body = (await (await GET()).json()) as Record<string, unknown>;

    expect(body.apiAuthoring).toBe(returned);
  });

  it('returns empty scopes when the policy gateway is unavailable', async () => {
    const payload = createSessionPayload();
    vi.mocked(getSession).mockResolvedValue(payload as unknown as IronSession<SessionPayload>);
    vi.mocked(isSessionValid).mockReturnValue(true);
    server.use(
      http.get('http://test-policy-gateway/policy/scopes', () => {
        throw new Error('Network error');
      }),
    );

    const res = await GET();

    expect(res.status).toBe(200);
    const body = (await res.json()) as Record<string, unknown>;
    expect((body.permissions as { scopes: string[] }).scopes).toEqual([]);
  });

  it('returns the eligible set with the active tenant marked', async () => {
    const payload = createSessionPayload({
      tenantId: 'kora-tenant-example',
      tenants: [
        { id: 'kora-tenant-example', displayName: 'Example Platform' },
        { id: 'kora-tenant-example-data', displayName: 'Example Data' },
      ],
    });
    vi.mocked(getSession).mockResolvedValue(payload as unknown as IronSession<SessionPayload>);
    vi.mocked(isSessionValid).mockReturnValue(true);

    const body = (await (await GET()).json()) as Record<string, unknown>;

    expect(body.tenants).toEqual([
      { id: 'kora-tenant-example', displayName: 'Example Platform', active: true },
      { id: 'kora-tenant-example-data', displayName: 'Example Data', active: false },
    ]);
  });

  it('does not expose accessToken, refreshToken, or tenantId in the response', async () => {
    // Security assertion: tokens and the session's tenantId must never leak to
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
