import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { config } from '@orbit/server/config';
import { parsePeriod } from '../../../../../(dashboard)/agents/_services/agents-insights-server';

type Params = { slug: string };

/**
 * GET /api/agents/types/usage/{slug}?period=24h|7d|30d|90d
 *
 * Proxies to the inference gateway's per-agent usage endpoint so the agent
 * overview page can show its token breakdown and spend card for a window. Usage is keyed
 * by agent slug, the identity the gateway attributes it to. The gateway counts
 * the agent only if the caller may see it, and a delegated agent's usage only
 * for the person who ran it. The session token is held server-side and never
 * reaches the browser. Visibility is enforced by the gateway, not by this route.
 */
export const GET = withAuth(
  async (req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { slug } = await (context?.params ?? Promise.resolve({ slug: '' }));
    if (!slug) {
      return NextResponse.json(
        { code: 'invalid_param', message: 'agent slug is required' },
        { status: 400 },
      );
    }

    const period = parsePeriod(req.nextUrl.searchParams.get('period'));
    if (period === null) {
      return NextResponse.json(
        { code: 'invalid_param', message: 'period must be one of 24h, 7d, 30d, 90d' },
        { status: 400 },
      );
    }

    const res = await fetch(
      `${config.inferenceGatewayUrl}/usage/agent-type/${encodeURIComponent(slug)}?period=${period}`,
      {
        headers: {
          Authorization: `Bearer ${session.accessToken}`,
          'Content-Type': 'application/json',
        },
        cache: 'no-store',
        signal: AbortSignal.timeout(10_000),
      },
    );

    if (!res.ok) {
      return NextResponse.json(
        { code: 'usage_unavailable', message: 'Failed to load agent type usage' },
        { status: res.status },
      );
    }

    return NextResponse.json(await res.json());
  },
);
