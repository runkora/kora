import 'server-only';
import { NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { listPlatformEventTypes } from '@runkora/server/triggers-client';

// GET /api/triggers/event-types: the platform event types an event trigger can listen for.
export const GET = withAuth(async (_req, session) => {
  const eventTypes = await listPlatformEventTypes(session.accessToken);
  return NextResponse.json({ data: eventTypes });
});
