import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { fetchUsageFromGateway } from '../../(dashboard)/model-gateway/_services/usage-server';
import { listPATsFromIdentity } from '../../(dashboard)/model-gateway/_services/tokens-server';

export const GET = withAuth(async (req: NextRequest, session) => {
  const sp = req.nextUrl.searchParams;
  const key = sp.get('key') ?? undefined;
  const cursor = sp.get('cursor') ?? undefined;
  const status = sp.get('status') ?? undefined;
  const modelId = sp.get('model_id') ?? undefined;
  const from = sp.get('from') ?? undefined;
  const to = sp.get('to') ?? undefined;

  const [usageResult, patsResult] = await Promise.all([
    fetchUsageFromGateway(session.accessToken, { tokenId: key, cursor, status, modelId, from, to }),
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
    hasNextPage: usageResult.hasNextPage,
    nextCursor: usageResult.nextCursor,
    limit: usageResult.limit,
    pats: patsResult.ok ? patsResult.tokens : [],
  });
});
