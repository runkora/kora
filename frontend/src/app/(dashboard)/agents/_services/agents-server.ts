import 'server-only';

import type { AgentType, Run } from '@orbit/shared/api';
import { agentsClient } from '@orbit/server/agents-client';
import { ApiError } from '@orbit/server/client';
import { toAgentTypeRow, toRunRow } from '../_types';
import type { AgentTypeRow, RunRow } from '../_types';

export type AgentsResult =
  | { ok: true; agentTypes: AgentTypeRow[]; runs: RunRow[] }
  | { ok: false; status: number; message: string };

export async function fetchAgentsFromBFF(
  accessToken: string,
  tenantId: string,
): Promise<AgentsResult> {
  try {
    const client = agentsClient(accessToken);
    const [typesUpstream, runsUpstream] = await Promise.all([
      client.get<{ agent_types: AgentType[] }>(
        `/api/v1/agent-types?tenant_id=${encodeURIComponent(tenantId)}`,
      ),
      client.get<{ runs: Run[] }>(`/api/v1/runs?tenant_id=${encodeURIComponent(tenantId)}`),
    ]);

    const agentTypes = (typesUpstream?.agent_types ?? []).map(toAgentTypeRow);
    const runs = (runsUpstream?.runs ?? []).map((r) => toRunRow(r, agentTypes));
    return { ok: true, agentTypes, runs };
  } catch (err) {
    if (err instanceof ApiError) {
      return { ok: false, status: err.status, message: err.message };
    }
    return { ok: false, status: 500, message: 'Failed to load agents' };
  }
}
