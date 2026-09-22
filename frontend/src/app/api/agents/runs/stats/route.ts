import 'server-only';
import { NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { agentsClient } from '@orbit/server/agents-client';
import { ApiError } from '@orbit/server/client';
import type { RunStats } from '@orbit/shared/api';

export const GET = withAuth(async (req, session) => {
  try {
    const client = agentsClient(session.accessToken);
    const params = new URLSearchParams({ tenant_id: session.tenantId });
    const agentTypeId = new URL(req.url).searchParams.get('agent_type_id');
    if (agentTypeId) params.set('agent_type_id', agentTypeId);

    const stats = await client.get<RunStats>(`/api/v1/runs/stats?${params.toString()}`);
    return NextResponse.json(stats);
  } catch (err) {
    if (err instanceof ApiError) {
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
    }
    throw err;
  }
});
