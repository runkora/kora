/**
 * Next.js Edge Middleware — authentication guard.
 *
 * Runs on every request matched by `config.matcher` (all routes except static
 * assets). For each request it:
 *
 *   1. Skips public paths (login page, BFF auth routes).
 *   2. Reads and unseals the iron-session cookie (Web Crypto API — Edge safe).
 *   3. Redirects to /login?next=<path> if no valid session exists.
 *   4. Attempts an inline token refresh when the access token expires within
 *      5 minutes, writing the updated session back to the response cookie.
 *      If the refresh fails the user is redirected to /login.
 *
 * IMPORTANT: Do NOT import `server-only` here — Edge Runtime does not run
 * Node.js built-ins. `iron-session` v8 uses the Web Crypto API exclusively.
 *
 * See ADR-0023 §Q3 and the NFR section in docs/prd/browser-auth-flow.md.
 */
import { getIronSession } from 'iron-session';
import { type NextRequest, NextResponse } from 'next/server';

import { getSessionOptions, type SessionPayload } from '@orbit/server/auth-config';
import { APP_ROUTES, PUBLIC_PATHS, isSafeRedirectPath } from '@orbit/lib/routing';

// ── Inline refresh ────────────────────────────────────────────────────────────

interface BackendSessionResponse {
  access_token: string;
  expires_in: number;
  refresh_token: string;
  issued_at: number;
}

/**
 * Attempts a best-effort inline token refresh against the Go identity service.
 * Returns the updated token data on success, or `null` on any failure.
 *
 * `process.env.ORBIT_API_URL` is available in Edge Runtime — it is a
 * server-side env var read at request time, not baked in at build time.
 */
async function tryRefresh(refreshToken: string): Promise<BackendSessionResponse | null> {
  const apiBase = process.env.ORBIT_API_URL?.replace(/\/$/, '');
  if (!apiBase) return null;

  try {
    const response = await fetch(`${apiBase}/identity/session`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({
        grant_type: 'refresh_token',
        refresh_token: refreshToken,
        // tenant_id is required by the backend. Hardcoded to 'default' for the
        // current single-tenant deployment; extend SessionPayload with tenantId
        // and read it here when multi-tenancy is introduced.
        tenant_id: 'default',
      }),
      cache: 'no-store',
      signal: AbortSignal.timeout(5_000),
    });

    if (!response.ok) return null;
    return (await response.json()) as BackendSessionResponse;
  } catch {
    return null;
  }
}

// ── Proxy ────────────────────────────────────────────────────────────────

export async function proxy(request: NextRequest): Promise<NextResponse> {
  const { pathname } = request.nextUrl;

  // 1. Skip public paths — no session check required.
  if (PUBLIC_PATHS.has(pathname)) {
    return NextResponse.next();
  }

  // 2. Read and unseal the session cookie.
  //    iron-session writes back to `response.cookies` when `session.save()` is
  //    called, so we pass in the mutable response cookies jar.
  const response = NextResponse.next();
  const sessionOptions = getSessionOptions();

  const session = await getIronSession<SessionPayload>(request, response, sessionOptions);

  // 3. No valid session — redirect to login, preserving the destination path.
  if (!session.user || !session.accessToken) {
    const loginUrl = new URL(APP_ROUTES.login, request.url);
    if (isSafeRedirectPath(pathname)) {
      loginUrl.searchParams.set('next', pathname);
    }
    return NextResponse.redirect(loginUrl);
  }

  // 4. Inline refresh when the token expires within 5 minutes.
  const nowSeconds = Math.floor(Date.now() / 1000);
  const secondsUntilExpiry = session.expiresAt - nowSeconds;

  if (secondsUntilExpiry < 5 * 60) {
    const refreshed = await tryRefresh(session.refreshToken);

    if (!refreshed) {
      // Refresh failed — destroy the stale session and redirect to login.
      // iron-session writes the cookie-clearing Set-Cookie header onto the
      // `response` object it was bound to. We copy those cookies onto the
      // redirect so the browser actually receives the delete instruction
      // and the stale cookie is cleared (preventing a redirect loop).
      session.destroy();
      const loginUrl = new URL(APP_ROUTES.login, request.url);
      if (isSafeRedirectPath(pathname)) {
        loginUrl.searchParams.set('next', pathname);
      }
      const loginRedirect = NextResponse.redirect(loginUrl);
      for (const cookie of response.cookies.getAll()) {
        loginRedirect.cookies.set(cookie);
      }
      return loginRedirect;
    }

    // Update the session in place — iron-session writes the sealed cookie into
    // `response.cookies` when `save()` is called.
    session.accessToken = refreshed.access_token;
    session.refreshToken = refreshed.refresh_token;
    session.expiresAt = refreshed.issued_at + refreshed.expires_in;
    await session.save();
  }

  return response;
}

// ── Matcher ───────────────────────────────────────────────────────────────────

/**
 * Run the middleware on all routes except Next.js internals and static files.
 * The auth API routes are in PUBLIC_PATHS and are skipped inside the function.
 */
export const config = {
  matcher: [
    /*
     * Match all request paths except:
     *   - _next/static  (static files)
     *   - _next/image   (image optimisation)
     *   - favicon.ico
     *   - Any file in the public/ directory (has a dot extension)
     */
    '/((?!_next/static|_next/image|favicon\\.ico|.*\\.(?:svg|png|jpg|jpeg|gif|webp|ico|css|js)$).*)',
  ],
};
