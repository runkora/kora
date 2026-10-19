/**
 * POST /api/auth/logout
 *
 * Revokes the kora tokens server-side and destroys the session cookie.
 *
 * 1. Reads the session from the sealed cookie.
 * 2. Calls `POST /identity/revoke` for both the refresh token and access token
 *    (best-effort — we still destroy the local session even if revocation fails).
 * 3. Destroys the iron-session cookie.
 * 4. Redirects to /login.
 *
 * RFC 7009 §2.2: the revocation endpoint always returns HTTP 200, whether the
 * token was valid, already expired, or unknown. Both cases are treated as
 * success here so a stale or already-revoked token never blocks logout.
 */
import 'server-only';

import { type NextRequest } from 'next/server';

import { config } from '@runkora/server/config';
import { getSession } from '@runkora/server/session';
import { APP_ROUTES } from '@runkora/lib/routing';

async function revokeToken(
  revokeUrl: string,
  token: string,
  tokenTypeHint: 'access_token' | 'refresh_token',
): Promise<void> {
  const body = new URLSearchParams({ token, token_type_hint: tokenTypeHint });
  const res = await fetch(revokeUrl, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: body.toString(),
    cache: 'no-store',
    signal: AbortSignal.timeout(5_000),
  });
  if (!res.ok) {
    throw new Error(`revocation endpoint returned ${res.status}`);
  }
}

export async function POST(request: NextRequest): Promise<Response> {
  const session = await getSession();

  if (session.user) {
    const revokeUrl = `${config.koraApiUrl}/identity/revoke`;

    // Fire revocation requests concurrently; don't block logout on failures.
    const [refreshResult, accessResult] = await Promise.allSettled([
      revokeToken(revokeUrl, session.refreshToken, 'refresh_token'),
      revokeToken(revokeUrl, session.accessToken, 'access_token'),
    ]);
    if (refreshResult.status === 'rejected') {
      console.error('[logout] refresh_token revocation failed:', refreshResult.reason);
    }
    if (accessResult.status === 'rejected') {
      console.error('[logout] access_token revocation failed:', accessResult.reason);
    }
  }

  // Destroy the local session cookie unconditionally.
  await session.destroy();

  return Response.redirect(new URL(APP_ROUTES.login, request.url));
}
