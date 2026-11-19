import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { fetchRunGrades } from '@runkora/server/agents-client';
import { ApiError } from '@runkora/server/client';

type Params = { runId: string };

// GET /api/agents/runs/{runId}/grades returns the run's grade per Grader. kora-agents applies the run read check.
export const GET = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { runId } = await (context?.params ?? Promise.resolve({ runId: '' }));
    try {
      return NextResponse.json(
        (await fetchRunGrades(session.accessToken, runId)) ?? { grades: [] },
      );
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);
