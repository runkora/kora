/**
 * POST /api/auth/login
 *
 * Initiates the OIDC Authorization Code flow with PKCE.
 *
 * 1. Accepts an optional `next` query parameter (safe relative path only).
 * 2. Calls `POST /identity/login` on the Go backend — the backend owns PKCE
 *    and returns a fully-formed `authorization_url`.
 * 3. Persists `next` in a short-lived HttpOnly cookie so it survives the
 *    OIDC round-trip (never stored in Redis — see ADR-0023).
 * 4. Returns HTTP 302 to the provider's authorization endpoint.
 */
import 'server-only';

import { cookies } from 'next/headers';
import { redirect } from 'next/navigation';
import { type NextRequest } from 'next/server';

import { config } from '@/server/config';
import { NEXT_COOKIE_MAX_AGE_SECONDS, NEXT_COOKIE_NAME } from '@/server/auth-config';
import { APP_ROUTES, isSafeRedirectPath } from '@/shared/routes';

interface LoginResponse {
  authorization_url: string;
}

export async function POST(request: NextRequest): Promise<Response> {
  // Resolve the post-login destination (validated, safe relative path only).
  const { searchParams } = request.nextUrl;
  const rawNext = searchParams.get('next') ?? '';
  const nextPath = isSafeRedirectPath(rawNext) ? rawNext : APP_ROUTES.home;

  // Derive the identity endpoint from ORBIT_API_URL.
  // e.g. "http://localhost:8080/api/v1" → "http://localhost:8080/api/v1/identity/login"
  const loginUrl = `${config.orbitApiUrl.replace(/\/$/, '')}/identity/login`;

  let authorizationUrl: string;

  try {
    const response = await fetch(loginUrl, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      // The Go backend requires a tenant_id. For now we use the well-known
      // default — multi-tenant picker is deferred (see OQ-05 in the PRD).
      body: JSON.stringify({ tenant_id: 'default' }),
      cache: 'no-store',
      signal: AbortSignal.timeout(10_000),
    });

    if (!response.ok) {
      const err = await response.json().catch(() => ({}));
      console.error('[auth/login] backend error', response.status, err);
      return Response.redirect(new URL(`${APP_ROUTES.login}?error=server_error`, request.url));
    }

    const data = (await response.json()) as LoginResponse;
    authorizationUrl = data.authorization_url;
  } catch (err) {
    console.error('[auth/login] fetch failed', err);
    return Response.redirect(new URL(`${APP_ROUTES.login}?error=server_error`, request.url));
  }

  // Persist the post-login destination in a short-lived HttpOnly cookie.
  // This survives the browser's OIDC round-trip without polluting the session.
  const cookieStore = await cookies();
  cookieStore.set(NEXT_COOKIE_NAME, nextPath, {
    httpOnly: true,
    sameSite: 'lax',
    secure: process.env.NODE_ENV === 'production',
    maxAge: NEXT_COOKIE_MAX_AGE_SECONDS,
    path: '/',
  });

  // Redirect the browser to the OIDC provider's authorization endpoint.
  redirect(authorizationUrl);
}
