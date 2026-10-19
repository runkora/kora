import 'server-only';
import { NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { listRoutingCandidates } from '@runkora/server/triggers-client';

// GET /api/triggers/routing/candidates: the agent types a routed trigger may
// name. The upstream marks each runnable against the caller's own claims.
export const GET = withAuth(async (_req, session) => {
  const candidates = await listRoutingCandidates(session.accessToken);
  return NextResponse.json({ data: candidates });
});
