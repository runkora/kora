import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { agentsClient } from '@orbit/server/agents-client';
import { ApiError } from '@orbit/server/client';
import type { Run, AgentType } from '@orbit/shared/api';

type Params = { runId: string };

export const GET = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { runId } = await (context?.params ?? Promise.resolve({ runId: '' }));
    try {
      const client = agentsClient(session.accessToken);
      const run = await client.get<Run>(`/api/v1/runs/${runId}`);
      if (!run) {
        return NextResponse.json({ code: 'not_found', message: 'Run not found' }, { status: 404 });
      }
      const agentType = await client
        .get<AgentType>(`/api/v1/agent-types/${run.agent_type_id}`)
        .catch(() => null);
      return NextResponse.json({ data: run, agent_type: agentType });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);

export const DELETE = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { runId } = await (context?.params ?? Promise.resolve({ runId: '' }));
    try {
      const client = agentsClient(session.accessToken);
      await client.delete(`/api/v1/runs/${runId}`);
      return new NextResponse(null, { status: 204 });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);
