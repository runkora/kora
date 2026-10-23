import type {
  SessionUser,
  SessionPayload,
  BackendSessionResponse,
} from '@runkora/server/auth-config';

// ── Helpers ───────────────────────────────────────────────────────────────────

/**
 * Builds a minimal but valid base64url-encoded JWT with the given payload.
 * The signature segment is fake — these tokens are never verified in tests.
 */
function buildFakeJwt(payload: Record<string, unknown>): string {
  const header = btoa(JSON.stringify({ alg: 'RS256', typ: 'JWT' }))
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '');
  const body = btoa(JSON.stringify(payload))
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '');
  return `${header}.${body}.fake-signature`;
}

// ── Factories ─────────────────────────────────────────────────────────────────

/**
 * Creates a `SessionUser` with sensible defaults.
 * Pass partial overrides to customise specific fields.
 */
export function createSessionUser(overrides: Partial<SessionUser> = {}): SessionUser {
  return {
    sub: 'user-test-123',
    email: 'dev@runkora.test',
    name: 'Dev User',
    ...overrides,
  };
}

/**
 * Creates a `SessionPayload` (the full iron-session cookie content).
 * The `accessToken` is a structurally valid but unsigned JWT whose payload
 * matches the user fields so that `decodeJwtPayload` in the session route
 * won't throw when these factories are used in POST /api/auth/session tests.
 */
export function createSessionPayload(overrides: Partial<SessionPayload> = {}): SessionPayload {
  const user = createSessionUser(overrides.user);
  const issuedAt = Math.floor(Date.now() / 1000);
  const expiresAt = issuedAt + 3600;

  const accessToken = buildFakeJwt({
    sub: user.sub,
    exp: expiresAt,
    kora: {
      display_name: user.name,
      claims: { email: user.email },
    },
  });

  return {
    user,
    accessToken,
    refreshToken: 'test-refresh-token',
    expiresAt,
    tenantId: 'test-tenant',
    ...overrides,
  };
}

/**
 * Creates a `BackendSessionResponse` — the shape returned by the Go identity
 * service at `POST /identity/session`. The `access_token` is a structurally
 * valid fake JWT so that the BFF route can decode it without throwing.
 *
 * Pass a `SessionUser` as the second argument to control the identity claims
 * embedded in the JWT. This avoids inline JWT construction in tests that need
 * to assert specific user fields extracted by the session route.
 */
export function createBackendSessionResponse(
  overrides: Partial<BackendSessionResponse> = {},
  user: SessionUser = createSessionUser(),
): BackendSessionResponse {
  const issuedAt = Math.floor(Date.now() / 1000);
  const expiresIn = 3600;
  const refreshTokenExpiresIn = 60 * 60 * 24 * 30;

  const accessToken = buildFakeJwt({
    sub: user.sub,
    exp: issuedAt + expiresIn,
    kora: {
      display_name: user.name,
      claims: { email: user.email },
    },
  });

  return {
    access_token: accessToken,
    token_type: 'Bearer',
    expires_in: expiresIn,
    refresh_token: 'test-refresh-token',
    issued_at: issuedAt,
    refresh_token_expires_in: refreshTokenExpiresIn,
    ...overrides,
  };
}
