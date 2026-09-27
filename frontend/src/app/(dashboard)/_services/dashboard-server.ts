import 'server-only';

import { fetchAgentsFromBFF } from '../agents/_services/agents-server';
import { fetchConnections } from '../connections/_services/connections-server';
import { fetchEnvironments } from '../environments/_services/environments-server';
import { fetchAnalyticsFromGateway } from '../model-gateway/_services/analytics-server';
import { fetchBudgetPrincipalsFromGateway } from '../model-gateway/_services/budgets-server';
import { EMPTY_RUN_STATS } from '../agents/_types';
import type { AgentTypeRow, RunRow } from '../agents/_types';
import type { RunStats } from '@orbit/shared/api';
import type { ConnectionRow } from '../connections/_types';
import type { EnvironmentRow } from '../environments/_types';
import type { AnalyticsResponse, TenantCapStatus } from '../model-gateway/_types';

/** Rolling window (in days) used for the dashboard's inference KPIs and trend. */
export const ANALYTICS_WINDOW_DAYS = 7;

export interface DashboardData {
  agentTypes: AgentTypeRow[];
  runs: RunRow[];
  runStats: RunStats;
  connections: ConnectionRow[];
  environments: EnvironmentRow[];
  /** Inference analytics over the last {@link ANALYTICS_WINDOW_DAYS} days, or null if unavailable. */
  analytics: AnalyticsResponse | null;
  /** Tenant-wide spend cap, or null when no cap is configured / the call fails. */
  tenantCap: TenantCapStatus | null;
}

/** The shape returned when there is no valid session — every section empty. */
export const EMPTY_DASHBOARD_DATA: DashboardData = {
  agentTypes: [],
  runs: [],
  runStats: EMPTY_RUN_STATS,
  connections: [],
  environments: [],
  analytics: null,
  tenantCap: null,
};

/**
 * Fetches all data needed by the dashboard home in parallel. Each sub-fetch
 * degrades independently — a failure in one source returns an empty list (or
 * null) for that section rather than failing the whole page.
 */
export async function fetchDashboardData(
  accessToken: string,
  tenantId: string,
): Promise<DashboardData> {
  const [agentsResult, connectionsResult, environmentsResult, analyticsResult, budgetResult] =
    await Promise.all([
      fetchAgentsFromBFF(accessToken, tenantId),
      fetchConnections(accessToken, tenantId),
      fetchEnvironments(accessToken),
      fetchAnalyticsFromGateway(accessToken, {
        days: ANALYTICS_WINDOW_DAYS,
        granularity: 'daily',
      }),
      fetchBudgetPrincipalsFromGateway(accessToken),
    ]);

  return {
    agentTypes: agentsResult.ok ? agentsResult.agentTypes : [],
    runs: agentsResult.ok ? agentsResult.recentRuns : [],
    runStats: agentsResult.ok ? agentsResult.runStats : EMPTY_RUN_STATS,
    connections: connectionsResult.ok ? connectionsResult.rows : [],
    environments: environmentsResult.ok ? environmentsResult.rows : [],
    analytics: analyticsResult.ok ? analyticsResult.data : null,
    tenantCap: budgetResult.ok ? (budgetResult.data.tenant_cap ?? null) : null,
  };
}
