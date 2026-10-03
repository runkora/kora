import 'server-only';
import { NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { loadBudgets } from '../../../../(dashboard)/agents/_services/agents-insights-server';

/**
 * GET /api/agents/insights/budgets
 *
 * Each visible agent's spend in its budget window beside its limit, plus the
 * tenant cap when the caller may read platform budgets.
 */
export const GET = withAuth(async (_req, session) => {
  const result = await loadBudgets(session.accessToken);
  if (!result.ok) {
    return NextResponse.json(
      { code: 'budgets_unavailable', message: 'Failed to load agent budgets' },
      { status: result.status },
    );
  }
  return NextResponse.json(result.data);
});
