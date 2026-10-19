import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { DEFAULT_PERIOD, isPeriod } from '../../../(dashboard)/agents/_lib/spend';
import { loadInsights } from '../../../(dashboard)/agents/_services/agents-insights-server';

/**
 * GET /api/agents/insights?period=24h|7d|30d|90d
 *
 * Usage, models, trend, budgets and run outcomes for one window.
 */
export const GET = withAuth(async (req: NextRequest, session) => {
  const period = req.nextUrl.searchParams.get('period') ?? DEFAULT_PERIOD;
  if (!isPeriod(period)) {
    return NextResponse.json(
      { code: 'invalid_param', message: 'period must be one of 24h, 7d, 30d, 90d' },
      { status: 400 },
    );
  }
  const result = await loadInsights(session.accessToken, period);
  if (!result.ok) {
    return NextResponse.json(
      { code: 'insights_unavailable', message: 'Failed to load agent insights' },
      { status: result.status },
    );
  }
  return NextResponse.json(result.data);
});
