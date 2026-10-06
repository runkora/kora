import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { DEFAULT_PERIOD, isPeriod } from '../../../../../(dashboard)/agents/_lib/spend';
import { loadAgentOverview } from '../../../../../(dashboard)/agents/[slug]/_services/agent-overview-server';

type Params = { slug: string };

/**
 * GET /api/agents/types/overview/{slug}?period=24h|7d|30d|90d
 *
 * The agent Overview's usage, spend, budget and run stats for one window. The
 * gateway and orbit-agents are read under the caller's token; a part either
 * refuses is left out of the response.
 */
export const GET = withAuth(
  async (req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { slug } = await (context?.params ?? Promise.resolve({ slug: '' }));
    const period = req.nextUrl.searchParams.get('period') ?? DEFAULT_PERIOD;
    if (!slug || !isPeriod(period)) {
      return NextResponse.json(
        {
          code: 'invalid_param',
          message: 'an agent slug and a period of 24h, 7d, 30d or 90d are required',
        },
        { status: 400 },
      );
    }
    return NextResponse.json(await loadAgentOverview(session.accessToken, slug, period));
  },
);
