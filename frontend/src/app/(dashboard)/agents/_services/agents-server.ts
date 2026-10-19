import 'server-only';

import type { AgentRunStats, AgentType } from '@runkora/shared/api';
import { agentsClient, fetchTenantRunStats, listRuns } from '@runkora/server/agents-client';
import { ApiError } from '@runkora/server/client';
import { DEFAULT_PERIOD } from '../_lib/spend';
import { indexAgentTypes, toAgentTypeRow, toRunRow } from '../_types';
import type { AgentTypeRow, RunRow } from '../_types';

export type AgentsResult =
  | { ok: true; agentTypes: AgentTypeRow[]; runs: RunRow[]; runStats: AgentRunStats | null }
  | { ok: false; status: number; message: string };

/**
 * The agent types, the first page of runs matching `runs`, and the tenant's
 * run stats for the default window. The stats are best-effort: null when they
 * cannot be read.
 */
export async function fetchAgentsFromBFF(
  accessToken: string,
  tenantId: string,
  runs: URLSearchParams,
): Promise<AgentsResult> {
  try {
    const [typesUpstream, page, runStats] = await Promise.all([
      agentsClient(accessToken).get<{ agent_types: AgentType[] }>(
        `/api/v1/agent-types?tenant_id=${encodeURIComponent(tenantId)}`,
      ),
      listRuns(accessToken, runs),
      fetchTenantRunStats(accessToken, DEFAULT_PERIOD).catch((err: unknown) => {
        console.error('[fetchAgentsFromBFF] run stats request failed', err);
        return undefined;
      }),
    ]);

    const agentTypes = (typesUpstream?.agent_types ?? []).map(toAgentTypeRow);
    const byId = indexAgentTypes(agentTypes);
    return {
      ok: true,
      agentTypes,
      runs: (page?.runs ?? []).map((r) => toRunRow(r, byId)),
      runStats: runStats ?? null,
    };
  } catch (err) {
    if (err instanceof ApiError) {
      return { ok: false, status: err.status, message: err.message };
    }
    return { ok: false, status: 500, message: 'Failed to load agents' };
  }
}
