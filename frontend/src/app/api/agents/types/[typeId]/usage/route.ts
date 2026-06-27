import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { config } from '@orbit/server/config';

type Params = { typeId: string };

/**
 * GET /api/agents/types/{typeId}/usage
 *
 * Proxies to the inference gateway's agent-type usage endpoint so the agent
 * overview page can show a 7-day token breakdown and spend card. The session
 * token is held server-side and never reaches the browser. Tenant scoping is
 * enforced by the gateway from the verified token, not by this route.
 */
export const GET = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { typeId } = await (context?.params ?? Promise.resolve({ typeId: '' }));
    if (!typeId) {
      return NextResponse.json(
        { code: 'invalid_param', message: 'agent type id is required' },
        { status: 400 },
      );
    }

    const res = await fetch(
      `${config.inferenceGatewayUrl}/usage/agent-type/${encodeURIComponent(typeId)}`,
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
