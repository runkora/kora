import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { MOCK_AGENT_TYPES } from '../../_mock/data';

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
    return NextResponse.json({ data: agent });
  },
);

export const DELETE = withAuth(
  async (_req: NextRequest, _session, context?: { params: Promise<{ typeId: string }> }) => {
    const { typeId } = await (context?.params ?? Promise.resolve({ typeId: '' }));
    const agent = MOCK_AGENT_TYPES.find((a) => a.id === typeId || a.slug === typeId);
    if (!agent) {
      return NextResponse.json(
        { code: 'not_found', message: 'Agent type not found' },
        { status: 404 },
      );
    }
    return new NextResponse(null, { status: 204 });
  },
);
