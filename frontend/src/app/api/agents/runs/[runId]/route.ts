import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { agentsClient } from '@orbit/server/agents-client';
import { ApiError } from '@orbit/server/client';
import { resolveUserDisplayNames } from '@orbit/server/services/identity-server';
import type { Run, RunWithAttribution, AgentType } from '@orbit/shared/api';

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
      const [agentType, names] = await Promise.all([
        client.get<AgentType>(`/api/v1/agent-types/${run.agent_type_id}`).catch(() => null),
        // Best-effort: unresolvable principals keep created_by_name null and the
        // UI falls back to the raw principal id.
        resolveUserDisplayNames(session.accessToken, [run.created_by]),
      ]);
      const data: RunWithAttribution = {
        ...run,
        created_by_name: names.get(run.created_by) ?? null,
      };
      return NextResponse.json({ data, agent_type: agentType });
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
