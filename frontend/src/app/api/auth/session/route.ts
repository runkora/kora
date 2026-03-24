/**
 * GET /api/auth/session
 *
 * Returns the public (non-sensitive) portion of the current session for use
 * by client components that need to know who is logged in.
 *
 * Returns:
 *   200 { user: SessionUser, expiresAt: number } — active session
 *   401 { error: 'unauthorized' }                — no session / expired
 *
 * The access token and refresh token are NEVER included in the response —
 * they live in the sealed HttpOnly cookie only.
 */
import 'server-only';

import { getSession, isSessionValid } from '@/server/session';

export async function GET(): Promise<Response> {
  const session = await getSession();

  if (!isSessionValid(session)) {
    return Response.json({ error: 'unauthorized' }, { status: 401 });
  }

  return Response.json({
    user: session.user,
    expiresAt: session.expiresAt,
  });
}
