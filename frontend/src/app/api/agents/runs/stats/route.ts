import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { fetchTenantRunStats } from '@orbit/server/agents-client';
import { ApiError } from '@orbit/server/client';
import { DEFAULT_PERIOD, isPeriod } from '../../../../(dashboard)/agents/_lib/spend';

/**
 * GET /api/agents/runs/stats?period=24h|7d|30d|90d
 *
 * Run counts and durations across every agent whose runs the caller may read.
 */
export const GET = withAuth(async (req: NextRequest, session) => {
  const period = req.nextUrl.searchParams.get('period') ?? DEFAULT_PERIOD;
  if (!isPeriod(period)) {
    return NextResponse.json(
      { code: 'invalid_param', message: 'period must be one of 24h, 7d, 30d, 90d' },
      { status: 400 },
    );
  }
  try {
    return NextResponse.json({
      data: (await fetchTenantRunStats(session.accessToken, period)) ?? null,
    });
  } catch (err) {
    if (err instanceof ApiError) {
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
    }
    throw err;
  }
});
