/**
 * GET /api/auth/callback
 *
 * OIDC Authorization Code callback handler.
 *
 * 1. Validates the provider's `?error` / `?state` / `?code` parameters.
 * 2. Calls `POST /identity/session` (authorization_code grant) on the Go
 *    backend — the backend verifies the PKCE state, exchanges the code, and
 *    returns Orbit JWTs.
 * 3. Decodes the access token (no re-verification — trust our own backend)
 *    to extract user identity for the session payload.
 * 4. Seals the session cookie via iron-session.
 * 5. Clears the short-lived `orbit_next` cookie and redirects to the
 *    preserved destination (or `/` on failure).
 *
 * See ADR-0023 for the Go-backend-owns-PKCE design decision.
 */
import 'server-only';

import { cookies } from 'next/headers';
import { redirect } from 'next/navigation';
import { type NextRequest } from 'next/server';

import { config } from '@/server/config';
import { getSession, type SessionPayload } from '@/server/session';
import { NEXT_COOKIE_NAME } from '@/server/auth-config';
import { APP_ROUTES, isSafeRedirectPath } from '@/shared/routes';

// ── Backend response shape ────────────────────────────────────────────────────

interface BackendSessionResponse {
  access_token: string;
  token_type: string;
  expires_in: number;
  refresh_token: string;
  issued_at: number; // Unix seconds
}

// ── JWT payload (lightweight decode — no verification needed server-side) ─────

interface OrbitJwtClaims {
  display_name?: string;
  claims?: Record<string, string>;
}

interface OrbitJwtPayload {
  sub: string;
  exp: number;
  orbit?: OrbitJwtClaims;
}

/**
 * Decodes the payload section of a JWT without verifying the signature.
 * Safe here because the token was just issued by our trusted Go backend over
 * a server-to-server connection.
 */
function decodeJwtPayload(token: string): OrbitJwtPayload | null {
  try {
    const parts = token.split('.');
    if (parts.length !== 3) return null;
    // atob is available in both Edge Runtime and Node.js ≥ 16.
    const payloadBase64 = parts[1].replace(/-/g, '+').replace(/_/g, '/');
    const json = atob(payloadBase64);
    return JSON.parse(json) as OrbitJwtPayload;
  } catch {
    return null;
  }
}

// ── Route handler ─────────────────────────────────────────────────────────────

export async function GET(request: NextRequest): Promise<Response> {
  const { searchParams, origin } = request.nextUrl;

  const errorParam = searchParams.get('error');
  const code = searchParams.get('code');
  const state = searchParams.get('state');

  // Provider returned an explicit error (e.g. access_denied, login_required).
  if (errorParam) {
    console.warn('[auth/callback] provider error:', errorParam);
    return redirect(`${APP_ROUTES.login}?error=${encodeURIComponent(errorParam)}`);
  }

  // `code` and `state` are both required for a valid callback.
  if (!code || !state) {
    console.warn('[auth/callback] missing code or state');
    return redirect(`${APP_ROUTES.login}?error=invalid_callback`);
  }

  // Read and clear the post-login destination cookie.
  const cookieStore = await cookies();
  const nextPath = cookieStore.get(NEXT_COOKIE_NAME)?.value ?? APP_ROUTES.home;
  const destination = isSafeRedirectPath(nextPath) ? nextPath : APP_ROUTES.home;

  // Exchange the authorization code for Orbit JWTs via the Go identity service.
  const sessionUrl = `${config.orbitApiUrl.replace(/\/$/, '')}/identity/session`;

  let sessionData: BackendSessionResponse;

  try {
    const response = await fetch(sessionUrl, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({
        grant_type: 'authorization_code',
        code,
        state,
        // tenant_id is required by the backend session builder.
        // Hardcoded to "default" until the multi-tenant picker is implemented (OQ-05).
        tenant_id: 'default',
      }),
      cache: 'no-store',
      signal: AbortSignal.timeout(10_000),
    });

    if (!response.ok) {
      const err = await response.json().catch(() => ({}));
      console.error('[auth/callback] backend session error', response.status, err);
      // Map known OAuth error codes; fall back to generic server_error.
      const errCode =
        (err as { code?: string }).code === 'invalid_field' ? 'invalid_request' : 'server_error';
      cookieStore.delete(NEXT_COOKIE_NAME);
      return redirect(`${APP_ROUTES.login}?error=${errCode}`);
    }

    sessionData = (await response.json()) as BackendSessionResponse;
  } catch (err) {
    console.error('[auth/callback] fetch failed', err);
    cookieStore.delete(NEXT_COOKIE_NAME);
    return redirect(`${APP_ROUTES.login}?error=server_error`);
  }

  // Decode the access token to extract user identity for the session payload.
  const jwt = decodeJwtPayload(sessionData.access_token);
  if (!jwt?.sub) {
    console.error('[auth/callback] could not decode access token');
    cookieStore.delete(NEXT_COOKIE_NAME);
    return redirect(`${APP_ROUTES.login}?error=server_error`);
  }

  const user: SessionPayload['user'] = {
    sub: jwt.sub,
    email: jwt.orbit?.claims?.['email'] ?? '',
    name: jwt.orbit?.display_name ?? jwt.orbit?.claims?.['email'] ?? jwt.sub,
  };

  const expiresAt = sessionData.issued_at + sessionData.expires_in;

  // Seal the session into the HttpOnly cookie.
  const session = await getSession();
  session.user = user;
  session.accessToken = sessionData.access_token;
  session.refreshToken = sessionData.refresh_token;
  session.expiresAt = expiresAt;
  await session.save();

  // Clear the short-lived `orbit_next` navigation cookie.
  cookieStore.delete(NEXT_COOKIE_NAME);

  // Redirect to the originally requested path (or home).
  redirect(`${origin}${destination}`);
}
