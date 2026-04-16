import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { fetchUsageFromGateway } from '../../(dashboard)/model-gateway/_services/usage-server';
import { listPATsFromIdentity } from '../../(dashboard)/model-gateway/_services/tokens-server';

export const GET = withAuth(async (req: NextRequest, session) => {
  const key = req.nextUrl.searchParams.get('key') ?? undefined;

  const [usageResult, patsResult] = await Promise.all([
    fetchUsageFromGateway(session.accessToken, { tokenId: key }),
    listPATsFromIdentity(session.accessToken),
  ]);

  if (!usageResult.ok) {
    console.error(`[GET /api/usage] upstream error ${usageResult.status}: ${usageResult.message}`);
    const clientStatus = usageResult.status >= 500 ? 502 : usageResult.status;
    return NextResponse.json(
      { code: 'upstream_error', message: 'Failed to load usage logs from inference gateway' },
      { status: clientStatus },
    );
  }

  return NextResponse.json({
    records: usageResult.records,
    total: usageResult.total,
    limit: usageResult.limit,
    offset: usageResult.offset,
    pats: patsResult.ok ? patsResult.tokens : [],
  });
});
