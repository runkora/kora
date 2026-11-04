import 'server-only';

import type { AgentType } from '@runkora/shared/api';
import { agentsClient } from '@runkora/server/agents-client';
import { ApiError } from '@runkora/server/client';
import { listRunsWithNames } from '@runkora/server/services/runs-server';
import { RUNS_PAGE_SIZE, indexAgentTypes, toAgentTypeRow, toRunRow } from '../../_types';
import type { AgentTypeRow, RunRow } from '../../_types';

export type AgentDetailResult =
  | { ok: true; agent: AgentTypeRow; runs: RunRow[]; nextCursor: string | null }
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

    // The list endpoint does not enrich budget (one policy-service call per
    // row); only the detail read carries the grant's budget. Fetch it so the
    // settings form and overview show the configured budget. Best-effort —
    // fall back to the list record if the detail read fails.
    const detail = await client.get<AgentType>(`/api/v1/agent-types/${found.id}`).catch(() => null);

    const agent = toAgentTypeRow(detail ?? found);

    // The first page of runs, with starters' display names so the runs tab attributes them on first paint.
    const page = await listRunsWithNames(
      accessToken,
      new URLSearchParams({ agent_type_id: found.id, limit: String(RUNS_PAGE_SIZE) }),
    );
    const byId = indexAgentTypes([agent]);
    const runs = page.data.map((r) => toRunRow(r, byId));

    return { ok: true, agent, runs, nextCursor: page.next_cursor };
  } catch (err) {
    if (err instanceof ApiError) {
      return { ok: false, status: err.status, message: err.message };
    }
    return { ok: false, status: 500, message: 'Failed to load agent' };
  }
}
