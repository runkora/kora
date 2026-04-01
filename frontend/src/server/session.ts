/**
 * Session helpers — server-side only.
 *
 * Wraps iron-session's `getIronSession` with the project's sealed cookie
 * configuration. Import from API routes and Server Components only — never
 * from client components or middleware (middleware uses the cookie directly).
 */
import 'server-only';

import { getIronSession, type IronSession } from 'iron-session';
import { cookies } from 'next/headers';

import { getSessionOptions, type SessionPayload } from '@orbit/server/auth-config';

// Re-export the payload type so callers only need one import.
export type { SessionPayload } from '@orbit/server/auth-config';

// ── Core helper ───────────────────────────────────────────────────────────────

/**
 * Returns the current request's iron-session instance.
 * In Next.js App Router, `cookies()` is the request-scoped cookie jar.
 *
 * Usage (Route Handler):
 * ```ts
 * const session = await getSession();
 * if (!session.user) return Response.json({ error: 'Unauthorized' }, { status: 401 });
 * ```
 */
export async function getSession(): Promise<IronSession<SessionPayload>> {
  const cookieStore = await cookies();
  return getIronSession<SessionPayload>(cookieStore, getSessionOptions());
}

// ── Convenience predicates ────────────────────────────────────────────────────

/**
 * Returns `true` when the session has a non-expired access token.
 * The check is intentionally permissive — clock drift of ≤30 s is tolerated.
 */
export function isSessionValid(session: IronSession<SessionPayload>): boolean {
  if (!session.user || !session.accessToken) return false;
  const nowSeconds = Math.floor(Date.now() / 1000);
  return session.expiresAt > nowSeconds - 30;
}

/**
 * Returns `true` when the access token expires within the next 5 minutes.
 * Used by middleware to decide whether to attempt an inline refresh.
 */
export function isSessionExpiringSoon(session: IronSession<SessionPayload>): boolean {
  if (!session.expiresAt) return true;
  const nowSeconds = Math.floor(Date.now() / 1000);
  return session.expiresAt - nowSeconds < 5 * 60;
}
