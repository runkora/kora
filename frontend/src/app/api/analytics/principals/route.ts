import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { fetchAnalyticsPrincipalsFromGateway } from '../../../(dashboard)/model-gateway/_services/analytics-server';
import { fetchAllTenantUsers } from '../../../(dashboard)/model-gateway/_services/tokens-server';
import type {
  AnalyticsPrincipalsPageResponse,
  PrincipalUsageRow,
  PrincipalUsageSummary,
} from '../../../(dashboard)/model-gateway/_types';

export const GET = withAuth(async (req: NextRequest, session) => {
  const sp = req.nextUrl.searchParams;
  const days = sp.get('days') ? Number(sp.get('days')) : undefined;
  const page = sp.get('page') ? Number(sp.get('page')) : undefined;
  const limit = sp.get('limit') ? Number(sp.get('limit')) : undefined;
  const sort_by = sp.get('sort_by') ?? undefined;
  const order = sp.get('order') ?? undefined;

  // Fetch usage data and all tenant users in parallel.
  const [result, allUsersResult] = await Promise.all([
    fetchAnalyticsPrincipalsFromGateway(session.accessToken, { days, page, limit, sort_by, order }),
    fetchAllTenantUsers(session.accessToken),
  ]);

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

  // Build an identity map from all tenant users.
  const userMap = new Map<string, { email: string; display_name: string }>();
  if (allUsersResult.ok) {
    allUsersResult.users.forEach((u) => userMap.set(u.id, u));
  }

  // Enrich usage rows with identity data.
  const usageRows: PrincipalUsageRow[] = result.data.data.map((row) => {
    const user = userMap.get(row.principal_id);
    return {
      ...row,
      email: user?.email ?? null,
      display_name: user?.display_name ?? null,
    };
  });

  // Add synthetic zero-activity rows for identity users not present in usage
  // data. Only injected when the full result fits on a single page (page 1 and
  // no further pages exist). When there are multiple pages, seenIds only covers
  // the current page — principals from earlier pages would get duplicate
  // zero-rows if we injected here.
  const isSinglePage = result.data.page === 1 && result.data.total <= result.data.limit;
  const seenIds = new Set(result.data.data.map((r) => r.principal_id));
  const syntheticRows: PrincipalUsageRow[] =
    isSinglePage && allUsersResult.ok
      ? allUsersResult.users
          .filter((u) => !seenIds.has(u.id))
          .map((u): PrincipalUsageRow => {
            const summary: PrincipalUsageSummary = {
              principal_id: u.id,
              principal_type: 'HUMAN',
              request_count: 0,
              total_tokens: 0,
              prompt_tokens: 0,
              completion_tokens: 0,
              cache_read_tokens: 0,
              cache_creation_tokens: 0,
              estimated_cost_usd: 0,
              error_rate: 0,
              avg_latency_ms: 0,
              last_active_at: '',
            };
            return { ...summary, email: u.email, display_name: u.display_name };
          })
      : [];

  const allRows = [...usageRows, ...syntheticRows];

  const response: AnalyticsPrincipalsPageResponse = {
    data: allRows,
    total: result.data.total,
    page: result.data.page,
    limit: result.data.limit,
  };

  return NextResponse.json(response);
});
