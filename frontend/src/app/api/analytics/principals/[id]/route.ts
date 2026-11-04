import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { fetchAnalyticsPrincipalByIdFromGateway } from '../../../../(dashboard)/model-gateway/_services/analytics-server';

type Params = { id: string };

// GET /api/analytics/principals/[id] — admin drill-down. Same response shape
// as /api/analytics/me, for the principal identified by `id` rather than the
// caller's own identity. The inference gateway enforces
// platform:analytics:read and always sources tenant_id from the caller's own
// verified JWT, so this route cannot be used to cross tenants regardless of
// the id supplied.
export const GET = withAuth(
  async (req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { id } = await (context?.params ?? Promise.resolve({ id: '' }));
    if (!id) {
      return NextResponse.json(
        { code: 'invalid_param', message: 'principal id is required' },
        {
          status: 400,
        },
      );
    }

    const sp = req.nextUrl.searchParams;
    const days = sp.get('days') ? Number(sp.get('days')) : undefined;
    const granularity = sp.get('granularity') ?? undefined;

    const result = await fetchAnalyticsPrincipalByIdFromGateway(session.accessToken, id, {
      days,
      granularity,
    });

    if (!result.ok) {
      console.error(
        `[GET /api/analytics/principals/:id] upstream error ${result.status}: ${result.message}`,
      );
      const clientStatus = result.status >= 500 ? 502 : result.status;
      return NextResponse.json(
        { code: 'upstream_error', message: 'Failed to load principal analytics' },
        { status: clientStatus },
      );
    }

    return NextResponse.json(result.data);
  },
);
