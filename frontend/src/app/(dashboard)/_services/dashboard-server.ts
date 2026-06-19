import 'server-only';

import { fetchAgentsFromBFF } from '../agents/_services/agents-server';
import { fetchConnections } from '../connections/_services/connections-server';
import { fetchEnvironments } from '../environments/_services/environments-server';
import type { AgentTypeRow, RunRow } from '../agents/_types';
import type { ConnectionRow } from '../connections/_types';
import type { EnvironmentRow } from '../environments/_types';

export interface DashboardData {
  agentTypes: AgentTypeRow[];
  runs: RunRow[];
  connections: ConnectionRow[];
  environments: EnvironmentRow[];
}

/**
 * Fetches all data needed by the dashboard home in parallel. Each sub-fetch
 * degrades independently — a failure in one source returns an empty list for
 * that section rather than failing the whole page.
 */
export async function fetchDashboardData(
  accessToken: string,
  tenantId: string,
): Promise<DashboardData> {
  const [agentsResult, connectionsResult, environmentsResult] = await Promise.all([
    fetchAgentsFromBFF(accessToken, tenantId),
    fetchConnections(accessToken, tenantId),
    fetchEnvironments(accessToken),
  ]);

  return {
    agentTypes: agentsResult.ok ? agentsResult.agentTypes : [],
    runs: agentsResult.ok ? agentsResult.runs : [],
    connections: connectionsResult.ok ? connectionsResult.rows : [],
    environments: environmentsResult.ok ? environmentsResult.rows : [],
  };
}
