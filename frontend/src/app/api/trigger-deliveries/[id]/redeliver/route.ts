import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { redeliverDelivery } from '@runkora/server/triggers-client';

type Params = { id: string };

// POST /api/trigger-deliveries/[id]/redeliver — replay a delivery (202).
//
// The response names a NEW delivery id: a replay is a fresh arrival that
// references the one it replays, so the caller follows the new id to see what
// it did rather than watching the original change.
export const POST = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { id } = await (context?.params ?? Promise.resolve({ id: '' }));
    const result = await redeliverDelivery(session.accessToken, id);
    return NextResponse.json({ data: result }, { status: 202 });
  },
);
