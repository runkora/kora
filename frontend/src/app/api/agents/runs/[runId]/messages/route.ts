import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { agentsClient } from '@runkora/server/agents-client';
import { ApiError } from '@runkora/server/client';

type Params = { runId: string };

export const POST = withAuth(
  async (req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { runId } = await (context?.params ?? Promise.resolve({ runId: '' }));
    const body = (await req.json()) as { content: string };
    if (!body.content?.trim()) {
      return NextResponse.json(
        { code: 'validation_error', message: 'content is required' },
        { status: 400 },
      );
    }
    try {
      const client = agentsClient(session.accessToken);
      await client.post(`/api/v1/runs/${runId}/messages`, { content: body.content });
      return new NextResponse(null, { status: 202 });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);
