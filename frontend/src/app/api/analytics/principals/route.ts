import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { fetchAnalyticsPrincipalsFromGateway } from '../../../(dashboard)/model-gateway/_services/analytics-server';
import { fetchUsersByIDs } from '../../../(dashboard)/model-gateway/_services/tokens-server';
import type {
  AnalyticsPrincipalsPageResponse,
  PrincipalUsageRow,
} from '../../../(dashboard)/model-gateway/_types';

export const GET = withAuth(async (req: NextRequest, session) => {
  const sp = req.nextUrl.searchParams;
  const days = sp.get('days') ? Number(sp.get('days')) : undefined;
  const page = sp.get('page') ? Number(sp.get('page')) : undefined;
  const limit = sp.get('limit') ? Number(sp.get('limit')) : undefined;

  const result = await fetchAnalyticsPrincipalsFromGateway(session.accessToken, {
    days,
    page,
    limit,
  });

  if (!result.ok) {
    console.error(
      `[GET /api/analytics/principals] upstream error ${result.status}: ${result.message}`,
    );
    const clientStatus = result.status >= 500 ? 502 : result.status;
    return NextResponse.json(
      { code: 'upstream_error', message: 'Failed to load principal analytics' },
      { status: clientStatus },
    );
  }

  // Enrich with identity data — resolve principal IDs to email + display_name.
  const principalIds = result.data.data.map((r) => r.principal_id);
  const usersResult = await fetchUsersByIDs(session.accessToken, principalIds);

  const userMap = new Map<string, { email: string; display_name: string }>();
  if (usersResult.ok) {
    usersResult.users.forEach((u) => userMap.set(u.id, u));
  }

  const enriched: PrincipalUsageRow[] = result.data.data.map((row) => {
    const user = userMap.get(row.principal_id);
    return {
      ...row,
      email: user?.email ?? null,
      display_name: user?.display_name ?? null,
    };
  });

  const response: AnalyticsPrincipalsPageResponse = {
    data: enriched,
    total: result.data.total,
    page: result.data.page,
    limit: result.data.limit,
  };

  return NextResponse.json(response);
});
