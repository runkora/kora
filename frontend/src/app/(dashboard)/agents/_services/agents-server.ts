import 'server-only';

import type { AgentRunStats, AgentType } from '@runkora/shared/api';
import { agentsClient, fetchTenantRunStats, listRuns } from '@runkora/server/agents-client';
import { loadFailure } from '@runkora/server/load-failure';
import type { Loaded } from '@runkora/server/load-failure';
import { DEFAULT_PERIOD } from '../_lib/spend';
import { indexAgentTypes, toAgentTypeRow, toRunRow } from '../_types';
import type { AgentTypeRow, RunRow } from '../_types';

export interface AgentsResult {
  agentTypes: Loaded<AgentTypeRow[]>;
  runs: Loaded<RunRow[]>;
  runStats: AgentRunStats | null;
}

/**
 * The agent types, the first page of runs matching `runs`, and the tenant's
 * run stats for the default window. Each loads on its own, so a caller who
 * may read agents but not runs still sees the agents. The stats are
 * best-effort: null when they cannot be read.
 */
export async function fetchAgentsFromBFF(
  accessToken: string,
  tenantId: string,
  runs: URLSearchParams,
): Promise<AgentsResult> {
  const [types, page, stats] = await Promise.allSettled([
    agentsClient(accessToken).get<{ agent_types: AgentType[] }>(
      `/api/v1/agent-types?tenant_id=${encodeURIComponent(tenantId)}`,
    ),
    listRuns(accessToken, runs),
    fetchTenantRunStats(accessToken, DEFAULT_PERIOD),
  ]);

  const agentTypes =
    types.status === 'fulfilled' ? (types.value?.agent_types ?? []).map(toAgentTypeRow) : [];
  const byId = indexAgentTypes(agentTypes);
  return {
    agentTypes:
      types.status === 'fulfilled'
        ? { ok: true, data: agentTypes }
        : loadFailure(types.reason, 'agents'),
    runs:
      page.status === 'fulfilled'
        ? { ok: true, data: (page.value?.runs ?? []).map((r) => toRunRow(r, byId)) }
        : loadFailure(page.reason, 'runs'),
    runStats: stats.status === 'fulfilled' ? (stats.value ?? null) : null,
  };
}
