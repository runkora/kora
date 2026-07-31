/**
 * BFF route helpers — server-side only.
 *
 * Provides `withAuth`, a higher-order function that wraps a Next.js Route
 * Handler, performs the session validity check once, and forwards the
 * validated session to the inner handler. Use this instead of repeating the
 * manual `getSession()` + `isSessionValid()` guard in every BFF route.
 *
 * Usage:
 * ```ts
 * export const GET = withAuth(async (req, session) => {
 *   const result = await fetchWidgetsFromGateway(session.accessToken);
 *   // ...
 * });
 * ```
 */
import 'server-only';

import { type NextRequest, NextResponse } from 'next/server';
import { type IronSession } from 'iron-session';

import { getSession, isSessionValid } from '@orbit/server/session';
import { type SessionPayload } from '@orbit/server/auth-config';
import { ApiError } from '@orbit/server/api-error';

// ── Types ─────────────────────────────────────────────────────────────────────

/**
 * A BFF handler that has already been authenticated. The validated session is
 * passed as the second argument — `session.accessToken` is guaranteed to be
 * present and non-expired. The optional third argument forwards Next.js dynamic
 * segment params for routes like `[id]` and `[runId]`.
 */
export type AuthedHandler<TParams = Record<string, string>> = (
  req: NextRequest,
  session: IronSession<SessionPayload>,
  context?: { params: Promise<TParams> },
) => Promise<NextResponse>;

// ── withAuth ──────────────────────────────────────────────────────────────────

/**
 * Wraps a BFF Route Handler with a session validity check.
 *
 * - Returns `401 { code: "unauthorized" }` if no valid session exists.
 * - Otherwise calls `handler(req, session, context)` with the validated session.
 * - An `ApiError` thrown by the handler (an upstream refusal surfaced by the
 *   typed server clients) becomes its own JSON response, code and status
 *   preserved. Handlers pass upstream verdicts through by not catching them;
 *   the catch-and-map block every route used to carry lives here once.
 *
 * The proxy (src/proxy.ts) already redirects unauthenticated *page* requests
 * to /login. This guard covers the case where a BFF API route is called
 * directly (e.g., client-side re-fetch) and the session has since expired.
 */
export function withAuth<TParams = Record<string, string>>(
  handler: AuthedHandler<TParams>,
): (req: NextRequest, context?: { params: Promise<TParams> }) => Promise<NextResponse> {
  return async (req: NextRequest, context?: { params: Promise<TParams> }) => {
    const session = await getSession();

    if (!isSessionValid(session)) {
      return NextResponse.json(
        { code: 'unauthorized', message: 'No valid session' },
        { status: 401 },
      );
    }

    try {
      return await handler(req, session, context);
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  };
}
