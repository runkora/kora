import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { listDeliveries } from '@orbit/server/triggers-client';

// GET /api/trigger-deliveries?source_id=&limit= — recent deliveries for the tenant.
export const GET = withAuth(async (req: NextRequest, session) => {
  const sourceId = req.nextUrl.searchParams.get('source_id') ?? undefined;
  const limitRaw = req.nextUrl.searchParams.get('limit');
  const limit = limitRaw ? Number(limitRaw) : undefined;
  const deliveries = await listDeliveries(session.accessToken, { sourceId, limit });
  return NextResponse.json({ data: deliveries });
});
