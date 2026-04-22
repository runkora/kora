import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { fetchAnalyticsFromGateway } from '../../(dashboard)/model-gateway/_services/analytics-server';

export const GET = withAuth(async (req: NextRequest, session) => {
  const sp = req.nextUrl.searchParams;
  const days = sp.get('days') ? Number(sp.get('days')) : undefined;
  const granularity = sp.get('granularity') ?? undefined;
  const modelId = sp.get('model_id') ?? undefined;

  const result = await fetchAnalyticsFromGateway(session.accessToken, {
    days,
    granularity,
    modelId,
  });

  if (!result.ok) {
    console.error(`[GET /api/analytics] upstream error ${result.status}: ${result.message}`);
    const clientStatus = result.status >= 500 ? 502 : result.status;
    return NextResponse.json(
      { code: 'upstream_error', message: 'Failed to load analytics from inference gateway' },
      { status: clientStatus },
    );
  }

  return NextResponse.json(result.data);
});
