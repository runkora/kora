/**
 * /api/events: BFF proxy for one page of the event history.
 *
 * GET ?category=&type=&agent=&principal=&q=&cursor=&limit=  (filters repeat)
 *     200 { data: EventPage }
 *     401 { code: 'unauthorized', message }
 *     4xx/5xx { code: 'upstream_error', message }
 */
import 'server-only';

import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { loadEventPage } from '../../(dashboard)/events/_services/events-server';

export const GET = withAuth(async (req: NextRequest, session) => {
  const result = await loadEventPage(session.accessToken, req.nextUrl.searchParams);
  if (!result.ok) {
    return NextResponse.json(
      { code: 'upstream_error', message: result.message },
      { status: result.status },
    );
  }
  return NextResponse.json({ data: result.data });
});
