import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { fetchAgentGraders } from '@runkora/server/agents-client';
import { ApiError } from '@runkora/server/client';

type Params = { typeId: string };

// GET /api/agents/types/{typeId}/graders returns the agent's Graders with their standing.
export const GET = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { typeId } = await (context?.params ?? Promise.resolve({ typeId: '' }));
    try {
      return NextResponse.json(
        (await fetchAgentGraders(session.accessToken, typeId)) ?? { graders: [] },
      );
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);
