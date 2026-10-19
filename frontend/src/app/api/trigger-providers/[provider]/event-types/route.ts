import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { providerEventTypes } from '@runkora/server/triggers-client';

type Params = { provider: string };

// GET /api/trigger-providers/[provider]/event-types: the provider's schema event types.
export const GET = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { provider } = await (context?.params ?? Promise.resolve({ provider: '' }));
    const eventTypes = await providerEventTypes(session.accessToken, provider);
    return NextResponse.json({ data: eventTypes });
  },
);
