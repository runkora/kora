import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { listDeliveries } from '@runkora/server/triggers-client';

// GET /api/trigger-deliveries?source_id=&trigger_id=&received_before=&limit= — recent deliveries
// for the tenant, newest first. `received_before` (RFC 3339) pages back from the oldest one held.
export const GET = withAuth(async (req: NextRequest, session) => {
  const sourceId = req.nextUrl.searchParams.get('source_id') ?? undefined;
  const triggerId = req.nextUrl.searchParams.get('trigger_id') ?? undefined;
  const receivedBefore = req.nextUrl.searchParams.get('received_before') ?? undefined;
  const limitRaw = req.nextUrl.searchParams.get('limit');
  const limit = limitRaw ? Number(limitRaw) : undefined;
  const deliveries = await listDeliveries(session.accessToken, {
    sourceId,
    triggerId,
    receivedBefore,
    limit,
  });
  return NextResponse.json({ data: deliveries });
});
