import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { listDeliveries } from '@runkora/server/triggers-client';

// GET /api/trigger-deliveries?source_id=&trigger_id=&agent_type_id=&status=&cursor=&limit=: one page of the
// tenant's deliveries, newest first. Echo `next_cursor` back as `cursor` for the next page.
export const GET = withAuth(async (req: NextRequest, session) => {
  const q = req.nextUrl.searchParams;
  const limitRaw = q.get('limit');
  const page = await listDeliveries(session.accessToken, {
    sourceId: q.get('source_id') ?? undefined,
    triggerId: q.get('trigger_id') ?? undefined,
    agentTypeId: q.get('agent_type_id') ?? undefined,
    status: q.get('status') ?? undefined,
    cursor: q.get('cursor') ?? undefined,
    limit: limitRaw ? Number(limitRaw) : undefined,
  });
  return NextResponse.json({ data: page });
});
