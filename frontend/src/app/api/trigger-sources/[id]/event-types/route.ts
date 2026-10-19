import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { sourceEventTypes } from '@runkora/server/triggers-client';

type Params = { id: string };

// GET /api/trigger-sources/[id]/event-types: what a trigger on this source can listen for.
export const GET = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { id } = await (context?.params ?? Promise.resolve({ id: '' }));
    const eventTypes = await sourceEventTypes(session.accessToken, id);
    return NextResponse.json({ data: eventTypes });
  },
);
