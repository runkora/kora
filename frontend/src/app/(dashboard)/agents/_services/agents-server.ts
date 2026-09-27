import 'server-only';

import type { AgentType, Run, RunStats } from '@orbit/shared/api';
import { agentsClient } from '@orbit/server/agents-client';
import { ApiError } from '@orbit/server/client';
import { EMPTY_RUN_STATS, indexAgentTypes, toAgentTypeRow, toRunRow } from '../_types';
import type { AgentTypeRow, RunRow } from '../_types';

/** Recent-runs feed size for the dashboard home page's event feed. */
const RECENT_RUNS_LIMIT = 8;

export type AgentsResult =
  | { ok: true; agentTypes: AgentTypeRow[]; runStats: RunStats; recentRuns: RunRow[] }
  | { ok: false; status: number; message: string };

/**
 * Fetches agent types plus a run summary in parallel. runStats covers the
 * dashboard's counts (active, completed, per-agent-type breakdown) without
 * fetching every run; recentRuns is a small bounded page (newest first) for
 * the home page's event feed, which needs actual rows rather than counts.
 */
export async function fetchAgentsFromBFF(
  accessToken: string,
  tenantId: string,
): Promise<AgentsResult> {
  try {
    const client = agentsClient(accessToken);
    const [typesUpstream, statsUpstream, runsUpstream] = await Promise.all([
      client.get<{ agent_types: AgentType[] }>(
        `/api/v1/agent-types?tenant_id=${encodeURIComponent(tenantId)}`,
      ),
      client.get<RunStats>(`/api/v1/runs/stats?tenant_id=${encodeURIComponent(tenantId)}`),
      client.get<{ runs: Run[] }>(
        `/api/v1/runs?tenant_id=${encodeURIComponent(tenantId)}&limit=${RECENT_RUNS_LIMIT}`,
      ),
    ]);

    const agentTypes = (typesUpstream?.agent_types ?? []).map(toAgentTypeRow);
    const byId = indexAgentTypes(agentTypes);
    const recentRuns = (runsUpstream?.runs ?? []).map((r) => toRunRow(r, byId));
    const runStats = statsUpstream ?? EMPTY_RUN_STATS;
    return { ok: true, agentTypes, runStats, recentRuns };
  } catch (err) {
    if (err instanceof ApiError) {
      return { ok: false, status: err.status, message: err.message };
    }
    return { ok: false, status: 500, message: 'Failed to load agents' };
  }
}
