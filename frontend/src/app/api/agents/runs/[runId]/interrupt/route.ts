import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { agentsClient } from '@orbit/server/agents-client';
import { ApiError } from '@orbit/server/client';

type Params = { runId: string };

export const POST = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { runId } = await (context?.params ?? Promise.resolve({ runId: '' }));
    try {
      const client = agentsClient(session.accessToken);
      await client.post(`/api/v1/runs/${runId}/interrupt`, null);
      return new NextResponse(null, { status: 202 });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);
