/**
 * POST /api/auth/logout
 *
 * Revokes the Orbit tokens server-side and destroys the session cookie.
 *
 * 1. Reads the session from the sealed cookie.
 * 2. Calls `POST /identity/revoke` for both the refresh token and access token
 *    (best-effort — we still destroy the local session even if revocation fails).
 * 3. Destroys the iron-session cookie.
 * 4. Redirects to /login.
 *
 * RFC 7009 §2.1: the revocation endpoint returns 200 even for unknown tokens
 * and 204 for invalid tokens — both are treated as success here.
 */
import 'server-only';

import { type NextRequest } from 'next/server';

import { config } from '@/server/config';
import { getSession } from '@/server/session';
import { APP_ROUTES } from '@/shared/routes';

async function revokeToken(
  revokeUrl: string,
  token: string,
  tokenTypeHint: 'access_token' | 'refresh_token',
): Promise<void> {
  const body = new URLSearchParams({ token, token_type_hint: tokenTypeHint });
  await fetch(revokeUrl, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: body.toString(),
    cache: 'no-store',
    signal: AbortSignal.timeout(5_000),
  });
  // Intentionally swallow errors — local session is destroyed regardless.
}

export async function POST(request: NextRequest): Promise<Response> {
  const session = await getSession();

  if (session.user) {
    const revokeUrl = `${config.orbitApiUrl.replace(/\/$/, '')}/identity/revoke`;

    // Fire revocation requests concurrently; don't block logout on failures.
    await Promise.allSettled([
      revokeToken(revokeUrl, session.refreshToken, 'refresh_token'),
      revokeToken(revokeUrl, session.accessToken, 'access_token'),
    ]);
  }

  // Destroy the local session cookie unconditionally.
  session.destroy();

  return Response.redirect(new URL(APP_ROUTES.login, request.url));
}
