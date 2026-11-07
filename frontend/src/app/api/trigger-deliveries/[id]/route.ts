import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { getDelivery } from '@runkora/server/triggers-client';

type Params = { id: string };

// GET /api/trigger-deliveries/[id]: one delivery, so a link opens it whatever the page range.
export const GET = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { id } = await (context?.params ?? Promise.resolve({ id: '' }));
    const delivery = await getDelivery(session.accessToken, id);
    return NextResponse.json({ data: delivery });
  },
);
