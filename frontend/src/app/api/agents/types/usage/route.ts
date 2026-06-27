import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { config } from '@orbit/server/config';

/**
 * GET /api/agents/types/usage
 *
 * Proxies to the inference gateway's per-agent-type usage aggregate so the
 * agents list can show total + per-agent 7-day spend and tokens. The session
 * token is held server-side and never reaches the browser. Tenant scoping is
 * enforced by the gateway from the verified token, not by this route.
 */
export const GET = withAuth(async (_req: NextRequest, session) => {
  const res = await fetch(`${config.inferenceGatewayUrl}/usage/agent-types`, {
    headers: {
      Authorization: `Bearer ${session.accessToken}`,
      'Content-Type': 'application/json',
    },
    cache: 'no-store',
    signal: AbortSignal.timeout(10_000),
  });

  if (!res.ok) {
    return NextResponse.json(
      { code: 'usage_unavailable', message: 'Failed to load agent type usage' },
      { status: res.status },
    );
  }

  return NextResponse.json(await res.json());
});
