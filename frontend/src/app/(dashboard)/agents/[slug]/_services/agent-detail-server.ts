import 'server-only';

import type { AgentType, Run } from '@orbit/shared/api';
import { agentsClient } from '@orbit/server/agents-client';
import { ApiError } from '@orbit/server/client';
import { indexAgentTypes, toAgentTypeRow, toRunRow } from '../../_types';
import type { AgentTypeRow, RunRow } from '../../_types';

export type AgentDetailResult =
  | { ok: true; agent: AgentTypeRow; runs: RunRow[] }
  | { ok: false; status: number; message: string };

export async function fetchAgentDetailFromBFF(
  slug: string,
  accessToken: string,
  tenantId: string,
): Promise<AgentDetailResult> {
  try {
    const client = agentsClient(accessToken);
    const typesUpstream = await client.get<{ agent_types: AgentType[] }>(
      `/api/v1/agent-types?tenant_id=${encodeURIComponent(tenantId)}`,
    );

    const found = (typesUpstream?.agent_types ?? []).find((a) => a.slug === slug);
    if (!found) {
      return { ok: false, status: 404, message: 'Agent not found' };
    }

    const agent = toAgentTypeRow(found);

    const runsUpstream = await client.get<{ runs: Run[] }>(
      `/api/v1/runs?tenant_id=${encodeURIComponent(tenantId)}&agent_type_id=${encodeURIComponent(found.id)}`,
    );
    const byId = indexAgentTypes([agent]);
    const runs = (runsUpstream?.runs ?? []).map((r) => toRunRow(r, byId));

    return { ok: true, agent, runs };
  } catch (err) {
    if (err instanceof ApiError) {
      return { ok: false, status: err.status, message: err.message };
    }
    return { ok: false, status: 500, message: 'Failed to load agent' };
  }
}
