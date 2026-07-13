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
 * See docs/adr/2026-04-01-inference-gateway-architecture.md §Q3 and the NFR section in docs/prd/browser-auth-flow.md.
 */
import { getIronSession } from 'iron-session';
import { type NextRequest, NextResponse } from 'next/server';

import {
  getSessionOptions,
  SESSION_COOKIE_NAME,
  type SessionPayload,
  type SessionPermissions,
  type BackendSessionResponse,
} from '@orbit/server/auth-config';
import { APP_ROUTES, PUBLIC_PATHS, isSafeRedirectPath, getDevEnvPrefix } from '@orbit/lib/routing';

// ── Inline refresh ────────────────────────────────────────────────────────────

/**
 * Attempts a best-effort inline token refresh against the Go identity service.
 * Returns the updated token data on success, or `null` on any failure.
 *
 * `process.env.ORBIT_API_URL` is available in Edge Runtime — it is a
 * server-side env var read at request time, not baked in at build time.
 */
async function tryRefresh(
  refreshToken: string,
  tenantId: string,
): Promise<BackendSessionResponse | null> {
  const apiBase = process.env.ORBIT_API_URL?.replace(/\/$/, '');
  if (!apiBase) return null;

  try {
    const response = await fetch(`${apiBase}/identity/session`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({
        grant_type: 'refresh_token',
        refresh_token: refreshToken,
        tenant_id: tenantId,
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

// ── Inline scopes refresh ────────────────────────────────────────────────────

/**
 * Fetches the principal's effective scope set from the policy gateway.
 * Returns `null` on any failure — the caller preserves the existing
 * permission set rather than clearing it.
 */
async function tryRefreshScopes(accessToken: string): Promise<SessionPermissions | null> {
  const policyBase = process.env.POLICY_GATEWAY_URL?.replace(/\/$/, '');
  if (!policyBase) return null;

  try {
    const res = await fetch(`${policyBase}/policy/scopes`, {
      method: 'GET',
      headers: {
        Authorization: `Bearer ${accessToken}`,
        Accept: 'application/json',
      },
      cache: 'no-store',
      signal: AbortSignal.timeout(5_000),
    });

    if (!res.ok) return null;

    const data = (await res.json()) as { scopes?: string[]; evaluated_at?: string };
    return {
      scopes: data.scopes ?? [],
      evaluatedAt: data.evaluated_at
        ? Math.floor(new Date(data.evaluated_at).getTime() / 1000)
        : Math.floor(Date.now() / 1000),
    };
  } catch {
    return null;
  }
}

// ── Login redirect ────────────────────────────────────────────────────────────

/**
 * Builds the redirect to /login for an unauthenticated request, restoring the
 * dev-environment path prefix (X-Forwarded-Prefix, stripped by the gateway
 * before this request reached the app — see getDevEnvPrefix) on both the
 * /login path itself and the `next` destination, so the browser round-trips
 * back through the environment's own /{env-id} gateway route rather than
 * landing on the unprefixed root. Outside a dev environment, prefix is "" and
 * this is unchanged from a plain /login redirect.
 */
function loginRedirectURL(request: NextRequest, pathname: string): URL {
  const prefix = getDevEnvPrefix(request.headers);
  const loginUrl = new URL(prefix + APP_ROUTES.login, request.url);
  if (isSafeRedirectPath(pathname)) {
    loginUrl.searchParams.set('next', prefix + pathname);
  }
  return loginUrl;
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
    return NextResponse.redirect(loginRedirectURL(request, pathname));
  }

  // 4. Inline refresh when the token expires within 5 minutes.
  const nowSeconds = Math.floor(Date.now() / 1000);
  const secondsUntilExpiry = session.expiresAt - nowSeconds;

  if (secondsUntilExpiry < 5 * 60) {
    const refreshed = await tryRefresh(session.refreshToken, session.tenantId);

    if (!refreshed) {
      // Only destroy the session if the token is truly expired. When the
      // backend is transiently unavailable within the 5-minute warning window,
      // the token is still valid — fall through and let the request proceed
      // rather than logging the user out for a transient failure.
      if (secondsUntilExpiry <= 0) {
        await session.destroy();
        const loginRedirect = NextResponse.redirect(loginRedirectURL(request, pathname));
        for (const cookie of response.cookies.getAll()) {
          loginRedirect.cookies.set(cookie);
        }
        return loginRedirect;
      }
      // Token still valid — pass through with stale data; refresh will be
      // retried on the next request.
      return response;
    }

    // Re-bind iron-session with options derived from the refreshed token TTL so
    // the cookie maxAge stays in sync with the new refresh token's expiry.
    // iron-session bakes maxAge at getIronSession() time, not at save() time,
    // so we must create a new session instance to pick up the updated value.
    const refreshedSession = await getIronSession<SessionPayload>(
      request,
      response,
      getSessionOptions(refreshed.refresh_token_expires_in),
    );
    refreshedSession.user = session.user;
    refreshedSession.tenantId = session.tenantId;
    refreshedSession.accessToken = refreshed.access_token;
    refreshedSession.refreshToken = refreshed.refresh_token;
    refreshedSession.expiresAt = refreshed.issued_at + refreshed.expires_in;

    // Refresh the permission set alongside the token. On failure, preserve
    // the existing permissions rather than clearing them (fail-open for refresh,
    // fail-closed only on initial login).
    const newPermissions = await tryRefreshScopes(refreshed.access_token);
    refreshedSession.permissions = newPermissions ??
      session.permissions ?? { scopes: [], evaluatedAt: 0 };

    await refreshedSession.save();

    // Forward the refreshed session cookie to downstream Server Components and
    // Route Handlers. Middleware writes to response.headers (→ Set-Cookie for
    // the browser) but cookies() from next/headers reads from the *request*
    // side. Without this, Server Components in the same render cycle still see
    // the expired token.
    //
    // iron-session writes via response.headers.append("set-cookie", ...) which
    // does NOT update response.cookies internal state — parse the raw header.
    let refreshedCookieValue: string | undefined;
    const setCookieHeader = response.headers.get('set-cookie') ?? '';
    const cookieMatch = setCookieHeader.match(
      new RegExp(`(?:^|,\\s*)${SESSION_COOKIE_NAME}=([^;,]+)`),
    );
    if (cookieMatch) {
      refreshedCookieValue = cookieMatch[1];
    }

    if (refreshedCookieValue) {
      const existingCookieHeader = request.headers.get('cookie') ?? '';
      const updatedCookieHeader = [
        ...existingCookieHeader
          .split(';')
          .map((c) => c.trim())
          .filter((c) => c && !c.startsWith(`${SESSION_COOKIE_NAME}=`)),
        `${SESSION_COOKIE_NAME}=${refreshedCookieValue}`,
      ].join('; ');

      const requestHeaders = new Headers(request.headers);
      requestHeaders.set('cookie', updatedCookieHeader);

      const forwardedResponse = NextResponse.next({ request: { headers: requestHeaders } });
      // Copy the Set-Cookie from the original response so the browser also
      // receives the refreshed cookie.
      const responseSetCookie = response.headers.get('set-cookie');
      if (responseSetCookie) {
        forwardedResponse.headers.set('set-cookie', responseSetCookie);
      }
      return forwardedResponse;
    }
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
