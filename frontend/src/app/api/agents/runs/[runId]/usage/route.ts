import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { config } from '@runkora/server/config';

type Params = { runId: string };

/**
 * GET /api/agents/runs/{runId}/usage
 *
 * Proxies to the inference gateway's per-run usage endpoint so the run chat
 * usage meter can show real spend + token totals. The session token is held
 * server-side and never reaches the browser. Tenant scoping is enforced by the
 * gateway from the verified token, not by this route.
 */
export const GET = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { runId } = await (context?.params ?? Promise.resolve({ runId: '' }));
    if (!runId) {
      return NextResponse.json(
        { code: 'invalid_param', message: 'run id is required' },
        { status: 400 },
      );
    }

    const res = await fetch(
      `${config.inferenceGatewayUrl}/usage/run/${encodeURIComponent(runId)}`,
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
        { code: 'usage_unavailable', message: 'Failed to load run usage' },
        { status: res.status },
      );
    }

    return NextResponse.json(await res.json());
  },
);
