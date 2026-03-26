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
}

// ── Cookie names ──────────────────────────────────────────────────────────────

/** Name of the sealed iron-session cookie. */
export const SESSION_COOKIE_NAME = 'orbit_session';

// ── Session options factory ───────────────────────────────────────────────────

/**
 * Returns iron-session options, reading `BFF_SESSION_SECRET` at call time.
 * Throws early with a descriptive error if the secret is absent or too short.
 */
export function getSessionOptions(): SessionOptions {
  const password = process.env.BFF_SESSION_SECRET;
  if (!password || password.length < 32) {
    throw new Error(
      'BFF_SESSION_SECRET must be set and at least 32 characters long. ' +
        'Generate one with: openssl rand -base64 32',
    );
  }
  return {
    cookieName: SESSION_COOKIE_NAME,
    password,
    cookieOptions: {
      secure: process.env.NODE_ENV === 'production',
      httpOnly: true,
      sameSite: 'lax' as const,
      // 7 days — mirrors the Go backend's refresh token TTL.
      maxAge: 60 * 60 * 24 * 7,
    },
  };
}
