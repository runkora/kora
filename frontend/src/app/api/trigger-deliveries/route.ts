import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { listDeliveries } from '@orbit/server/triggers-client';

// One page of deliveries for the tenant, newest first. status and outcome repeat
// (?status=failed&status=missed) rather than taking a comma list, so they pass
// straight through to the platform API.
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
    omitPayload: q.get('omit_payload') === 'true',
  });
  return NextResponse.json({ data: page.deliveries, next_cursor: page.nextCursor });
});
