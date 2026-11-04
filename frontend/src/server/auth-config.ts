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
  /** kora principal ID (JWT `sub` claim). */
  sub: string;
  email: string;
  name: string;
}

/**
 * Shape of the data sealed into the `kora_session` HttpOnly cookie.
 * The access token is kept server-side only — it is never sent to the browser.
 */
export interface SessionPayload {
  user: SessionUser;
  /** kora platform access token (JWT). */
  accessToken: string;
  /** Opaque refresh token used to renew the access token. */
  refreshToken: string;
  /** UTC Unix seconds when the access token expires. */
  expiresAt: number;
  /** Tenant ID the session was created against (used for token refresh). */
  tenantId: string;
  /**
   * False when the tenant takes its changes from its configuration repository.
   * Absent on sessions sealed before the field existed, which read as true.
   */
  apiAuthoring?: boolean;
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
  /** False when the caller's tenant takes its changes from its configuration repository. */
  api_authoring?: boolean;
}

/**
 * Reads the tenant a kora access token was issued for (`kora.tenant_id`).
 * The token is authoritative: a claim binding can move the person to another
 * tenant than the one the request named. The payload is not verified; the token
 * comes straight from the identity service. Returns undefined when absent.
 */
export function tenantIdFromAccessToken(token: string): string | undefined {
  try {
    const segment = token.split('.')[1] ?? '';
    const base64 = segment
      .replace(/-/g, '+')
      .replace(/_/g, '/')
      .padEnd(Math.ceil(segment.length / 4) * 4, '=');
    const payload = JSON.parse(atob(base64)) as { kora?: { tenant_id?: unknown } };
    const id = payload.kora?.tenant_id;
    return typeof id === 'string' && id ? id : undefined;
  } catch {
    return undefined;
  }
}

// ── Cookie names ──────────────────────────────────────────────────────────────

/** Name of the sealed iron-session cookie. */
export const SESSION_COOKIE_NAME = 'kora_session';

/**
 * True when the console's public URL is http:// on localhost or *.localhost.
 * Safari drops Secure cookies on that origin even though it is a secure context.
 * The URL comes from KORA_APP_URL, a runtime value: NEXT_PUBLIC_APP_URL is
 * inlined at build time and would always hold the image's build value.
 */
function isInsecureLoopbackUrl(appUrl: string | undefined): boolean {
  if (!appUrl) return false;
  try {
    const { protocol, hostname } = new URL(appUrl);
    return protocol === 'http:' && (hostname === 'localhost' || hostname.endsWith('.localhost'));
  } catch {
    return false;
  }
}

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
      secure:
        process.env.NODE_ENV === 'production' && !isInsecureLoopbackUrl(process.env.KORA_APP_URL),
      httpOnly: true,
      // lax (not strict) so the cookie is sent after the cross-origin OIDC
      // redirect back from the IdP (strict blocks that first same-site navigation
      // in Safari / browsers that enforce the cross-site-context rule).
      sameSite: 'lax' as const,
      maxAge,
    },
  };
}
