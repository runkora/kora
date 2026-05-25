import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { MOCK_RUNS, MOCK_AGENT_TYPES } from '../../../_mock/data';

export const GET = withAuth(
  async (_req: NextRequest, _session, context?: { params: Promise<{ typeId: string }> }) => {
    const { typeId } = await (context?.params ?? Promise.resolve({ typeId: '' }));
    const agent = MOCK_AGENT_TYPES.find((a) => a.id === typeId || a.slug === typeId);
    if (!agent) {
      return NextResponse.json(
        { code: 'not_found', message: 'Agent type not found' },
        { status: 404 },
      );
    }
    const runs = MOCK_RUNS.filter((r) => r.agent_type_id === agent.id);
    return NextResponse.json({ data: runs });
  },
);

export const POST = withAuth(
  async (_req: NextRequest, _session, context?: { params: Promise<{ typeId: string }> }) => {
    const { typeId } = await (context?.params ?? Promise.resolve({ typeId: '' }));
    const agent = MOCK_AGENT_TYPES.find((a) => a.id === typeId || a.slug === typeId);
    if (!agent) {
      return NextResponse.json(
        { code: 'not_found', message: 'Agent type not found' },
        { status: 404 },
      );
    }
    // Return the canonical demo run. The UI navigates to /agents/{slug}/runs/run_demo_chat_01.
    const run = {
      id: 'run_demo_chat_01',
      tenant_id: agent.tenant_id,
      agent_type_id: agent.id,
      state: 'pending' as const,
      pod_ordinal: -1,
      created_by: 'user_01jwk4m2n3p5q7r8',
      started_at: null,
      completed_at: null,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };
    return NextResponse.json({ data: run }, { status: 201 });
  },
);
