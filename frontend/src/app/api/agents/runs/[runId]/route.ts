import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { MOCK_RUNS, MOCK_AGENT_TYPES } from '../../_mock/data';

export const GET = withAuth(
  async (_req: NextRequest, _session, context?: { params: Promise<{ runId: string }> }) => {
    const { runId } = await (context?.params ?? Promise.resolve({ runId: '' }));
    const run = MOCK_RUNS.find((r) => r.id === runId);
    if (!run) {
      return NextResponse.json({ code: 'not_found', message: 'Run not found' }, { status: 404 });
    }
    const agentType = MOCK_AGENT_TYPES.find((a) => a.id === run.agent_type_id) ?? null;
    return NextResponse.json({ data: run, agent_type: agentType });
  },
);

export const DELETE = withAuth(
  async (_req: NextRequest, _session, context?: { params: Promise<{ runId: string }> }) => {
    const { runId } = await (context?.params ?? Promise.resolve({ runId: '' }));
    const run = MOCK_RUNS.find((r) => r.id === runId);
    if (!run) {
      return NextResponse.json({ code: 'not_found', message: 'Run not found' }, { status: 404 });
    }
    return new NextResponse(null, { status: 204 });
  },
);
