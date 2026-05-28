/**
 * Auth configuration shared between server-only route handlers and middleware.
 *
 * Sensitive env vars are read lazily at request time via `getSessionOptions()`
 * so build-time environments without secrets don't crash. The `server-only`
 * guard prevents accidental client-bundle inclusion without blocking the Edge
 * Runtime — Edge middleware is server-side and is not affected by this import.
 */
import 'server-only';

import type { SessionOptions } from 'iron-session';

// ── Types ─────────────────────────────────────────────────────────────────────

/** User identity stored inside the sealed session cookie. */
export interface SessionUser {
  /** Orbit principal ID (JWT `sub` claim). */
  sub: string;
  email: string;
  name: string;
}

/** Cached permission projection from the policy gateway's OPA evaluation. */
export interface SessionPermissions {
  /** Complete set of scopes the principal holds (grants + roles). */
  scopes: string[];
  /** UTC Unix seconds when the scopes were last evaluated. */
  evaluatedAt: number;
}

/**
 * Shape of the data sealed into the `orbit_session` HttpOnly cookie.
 * The access token is kept server-side only — it is never sent to the browser.
 */
export interface SessionPayload {
  user: SessionUser;
  /** Orbit platform access token (JWT). */
  accessToken: string;
  /** Opaque refresh token used to renew the access token. */
  refreshToken: string;
  /** UTC Unix seconds when the access token expires. */
  expiresAt: number;
  /** Tenant ID the session was created against (used for token refresh). */
  tenantId: string;
  /** Cached permission set from the policy gateway. Empty scopes = fail-closed. */
  permissions: SessionPermissions;
}

// ── Backend API response shapes ──────────────────────────────────────────────

/** Shape returned by Go `POST /identity/session` for all grant types. */
export interface BackendSessionResponse {
  access_token: string;
  token_type: string;
  /** Access token lifetime in seconds. */
  expires_in: number;
  refresh_token: string;
  issued_at: number;
  /**
   * Refresh token lifetime in seconds.
   * When present, the BFF uses this to set the session cookie maxAge so the
   * cookie stays alive for exactly as long as the refresh token is valid.
   */
  refresh_token_expires_in?: number;
}

// ── Cookie names ──────────────────────────────────────────────────────────────

/** Name of the sealed iron-session cookie. */
export const SESSION_COOKIE_NAME = 'orbit_session';

// ── Session options factory ───────────────────────────────────────────────────

/**
 * Returns iron-session options, reading `BFF_SESSION_SECRET` at call time.
 * Throws early with a descriptive error if the secret is absent or too short.
 *
 * @param refreshTokenExpiresIn - Refresh token lifetime in seconds, as
 *   returned by the backend in `refresh_token_expires_in`. When provided the
 *   cookie maxAge is derived from the actual token TTL so the two stay in
 *   sync. Falls back to 30 days if the backend does not supply the value.
 */
export function getSessionOptions(refreshTokenExpiresIn?: number): SessionOptions {
  const password = process.env.BFF_SESSION_SECRET;
  if (!password || password.length < 32) {
    throw new Error(
      'BFF_SESSION_SECRET must be set and at least 32 characters long. ' +
        'Generate one with: openssl rand -base64 32',
    );
  }

  // Default to 30 days — matches the Go backend's default RefreshTokenTTLDays.
  // Use || not ?? so that 0 (misconfigured backend) also falls back to the default.
  const maxAge = refreshTokenExpiresIn || 60 * 60 * 24 * 30;

  return {
    cookieName: SESSION_COOKIE_NAME,
    password,
    cookieOptions: {
      secure: process.env.NODE_ENV === 'production',
      httpOnly: true,
      // lax (not strict) so the cookie is sent after the cross-origin OIDC
      // redirect back from the IdP (strict blocks that first same-site navigation
      // in Safari / browsers that enforce the cross-site-context rule).
      sameSite: 'lax' as const,
      maxAge,
    },
  };
}
