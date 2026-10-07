import 'server-only';
import { NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { ApiError } from '@orbit/server/client';
import { listRunsWithNames, pickRunsPageParams } from '@orbit/server/services/runs-server';

// GET /api/agents/runs?agent_type_id=&state=&limit=&cursor= returns one page of runs.
export const GET = withAuth(async (req, session) => {
  try {
    const params = pickRunsPageParams(new URL(req.url).searchParams);
    return NextResponse.json(await listRunsWithNames(session.accessToken, params));
  } catch (err) {
    if (err instanceof ApiError) {
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
    }
    throw err;
  }
});
