import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { listDeliveries } from '@orbit/server/triggers-client';

// GET /api/trigger-deliveries?source_id=&trigger_id=&kind=&status=&outcome=&limit=&cursor=
// One page of deliveries for the tenant, newest first.
//
// status and outcome repeat (?status=failed&status=missed) rather than taking a
// comma list, so they pass straight through to the platform API. next_cursor
// rides alongside data: it belongs to the page, not to any one delivery.
export const GET = withAuth(async (req: NextRequest, session) => {
  const q = req.nextUrl.searchParams;
  const limitRaw = q.get('limit');
  const page = await listDeliveries(session.accessToken, {
    sourceId: q.get('source_id') ?? undefined,
    triggerId: q.get('trigger_id') ?? undefined,
    kind: q.get('kind') ?? undefined,
    status: q.getAll('status'),
    outcome: q.getAll('outcome'),
    limit: limitRaw ? Number(limitRaw) : undefined,
    cursor: q.get('cursor'),
  });
  return NextResponse.json({ data: page.deliveries, next_cursor: page.nextCursor });
});
