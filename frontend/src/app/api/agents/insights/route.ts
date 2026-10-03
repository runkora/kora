import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import {
  loadInsights,
  parsePeriod,
} from '../../../(dashboard)/agents/_services/agents-insights-server';

/**
 * GET /api/agents/insights?period=24h|7d|30d|90d
 *
 * Usage, model split, spend over time and run outcomes for the agents the
 * caller may see. The inference gateway and orbit-agents decide which agents
 * those are from the session token, which stays on the server.
 */
export const GET = withAuth(async (req: NextRequest, session) => {
  const period = parsePeriod(req.nextUrl.searchParams.get('period'));
  if (period === null) {
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
