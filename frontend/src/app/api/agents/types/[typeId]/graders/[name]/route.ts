import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { fetchAgentGrader } from '@runkora/server/agents-client';
import { ApiError } from '@runkora/server/client';

type Params = { typeId: string; name: string };

// GET /api/agents/types/{typeId}/graders/{name} returns one Grader with its spec and flagged runs.
export const GET = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { typeId, name } = await (context?.params ?? Promise.resolve({ typeId: '', name: '' }));
    try {
      const grader = await fetchAgentGrader(session.accessToken, typeId, name);
      if (!grader) {
        return NextResponse.json(
          { code: 'not_found', message: 'Grader not found' },
          { status: 404 },
        );
      }
      return NextResponse.json(grader);
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);
