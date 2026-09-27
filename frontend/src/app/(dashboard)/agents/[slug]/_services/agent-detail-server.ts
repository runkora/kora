import 'server-only';

import type { AgentType, Run, RunStats } from '@orbit/shared/api';
import { agentsClient } from '@orbit/server/agents-client';
import { ApiError } from '@orbit/server/client';
import { resolveUserDisplayNames } from '@orbit/server/services/identity-server';
import {
  EMPTY_RUN_STATS,
  RUNS_PAGE_SIZE,
  indexAgentTypes,
  toAgentTypeRow,
  toRunRow,
} from '../../_types';
import type { AgentTypeRow, RunRow } from '../../_types';

export type AgentDetailResult =
  | {
      ok: true;
      agent: AgentTypeRow;
      runs: RunRow[];
      hasMoreRuns: boolean;
      nextCursor: string | null;
      runStats: RunStats;
    }
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

    const [runsUpstream, runStats] = await Promise.all([
      client.get<{ runs: Run[]; has_more: boolean; next_cursor: string | null }>(
        `/api/v1/runs?tenant_id=${encodeURIComponent(tenantId)}&agent_type_id=${encodeURIComponent(found.id)}&limit=${RUNS_PAGE_SIZE}`,
      ),
      client.get<RunStats>(`/api/v1/runs/stats?agent_type_id=${encodeURIComponent(found.id)}`),
    ]);
    const rawRuns = runsUpstream?.runs ?? [];
    // Resolve run starters to display names (same enrichment as the runs BFF
    // routes) so the server-rendered runs tab attributes runs on first paint.
    const names = await resolveUserDisplayNames(
      accessToken,
      rawRuns.map((r) => r.created_by),
    );
    const byId = indexAgentTypes([agent]);
    const runs = rawRuns.map((r) =>
      toRunRow({ ...r, created_by_name: names.get(r.created_by) ?? null }, byId),
    );

    return {
      ok: true,
      agent,
      runs,
      hasMoreRuns: runsUpstream?.has_more ?? false,
      nextCursor: runsUpstream?.next_cursor ?? null,
      runStats: runStats ?? EMPTY_RUN_STATS,
    };
  } catch (err) {
    if (err instanceof ApiError) {
      return { ok: false, status: err.status, message: err.message };
    }
    return { ok: false, status: 500, message: 'Failed to load agent' };
  }
}
