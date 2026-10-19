import 'server-only';

import { fetchAgentsFromBFF } from '../agents/_services/agents-server';
import { loadFailure } from '@runkora/server/load-failure';
import { fetchConnections } from '../connections/_services/connections-server';
import { listEnvironments } from '@runkora/server/environments-client';
import { toEnvironmentRow } from '../environments/_types';
import { fetchAnalyticsFromGateway } from '../model-gateway/_services/analytics-server';
import { fetchBudgetPrincipalsFromGateway } from '../model-gateway/_services/budgets-server';
import type { RunStateCounts } from '@runkora/shared/api';
import type { AgentTypeRow, RunRow } from '../agents/_types';
import type { ConnectionRow } from '../connections/_types';
import type { EnvironmentRow } from '../environments/_types';
import type { AnalyticsResponse, TenantCapStatus } from '../model-gateway/_types';

/** Rolling window (in days) used for the dashboard's inference KPIs and trend. */
export const ANALYTICS_WINDOW_DAYS = 7;

/** How many of the newest runs the Recent Runs card lists. */
export const RECENT_RUNS = 8;

export interface DashboardData {
  agentTypes: AgentTypeRow[];
  /** The newest {@link RECENT_RUNS} runs. */
  recentRuns: RunRow[];
  /** Every run counted by current state, or null when the counts could not be read. */
  runCounts: RunStateCounts | null;
  connections: ConnectionRow[];
  environments: EnvironmentRow[];
  /** Inference analytics over the last {@link ANALYTICS_WINDOW_DAYS} days, or null if unavailable. */
  analytics: AnalyticsResponse | null;
  /** Tenant-wide spend cap, or null when no cap is configured / the call fails. */
  tenantCap: TenantCapStatus | null;
  /** Why a section could not be read, so the page says so instead of showing it empty. */
  agentsError: string | null;
  runsError: string | null;
  environmentsError: string | null;
  connectionsError: string | null;
  budgetError: string | null;
}

/** The shape returned when there is no valid session — every section empty. */
export const EMPTY_DASHBOARD_DATA: DashboardData = {
  agentTypes: [],
  recentRuns: [],
  runCounts: null,
  connections: [],
  environments: [],
  analytics: null,
  tenantCap: null,
  agentsError: null,
  runsError: null,
  environmentsError: null,
  connectionsError: null,
  budgetError: null,
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
  const [agentsResult, connectionsResult, environments, analyticsResult, budgetResult] =
    await Promise.all([
      fetchAgentsFromBFF(
        accessToken,
        tenantId,
        new URLSearchParams({ limit: String(RECENT_RUNS) }),
      ),
      fetchConnections(accessToken, tenantId),
      listEnvironments(accessToken)
        .then((envs) => ({ ok: true as const, data: envs.map(toEnvironmentRow) }))
        .catch((err: unknown) => loadFailure(err, 'environments')),
      fetchAnalyticsFromGateway(accessToken, {
        days: ANALYTICS_WINDOW_DAYS,
        granularity: 'daily',
      }),
      fetchBudgetPrincipalsFromGateway(accessToken),
    ]);

  const { agentTypes, runs } = agentsResult;
  return {
    agentTypes: agentTypes.ok ? agentTypes.data : [],
    recentRuns: runs.ok ? runs.data : [],
    runCounts: agentsResult.runStats?.current ?? null,
    connections: connectionsResult.ok ? connectionsResult.rows : [],
    environments: environments.ok ? environments.data : [],
    analytics: analyticsResult.ok ? analyticsResult.data : null,
    tenantCap: budgetResult.ok ? (budgetResult.data.tenant_cap ?? null) : null,
    agentsError: agentTypes.ok ? null : agentTypes.message,
    runsError: runs.ok ? null : runs.message,
    environmentsError: environments.ok ? null : environments.message,
    connectionsError: connectionsResult.ok ? null : connectionsResult.message,
    budgetError: budgetResult.ok ? null : budgetResult.message,
  };
}
