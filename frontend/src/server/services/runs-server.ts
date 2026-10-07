import 'server-only';

import { listRuns } from '@orbit/server/agents-client';
import { resolveUserDisplayNames } from '@orbit/server/services/identity-server';
import type { RunsPageResponse } from '@orbit/shared/api';

/** The run list params a BFF route passes through to orbit-agents, which validates them. */
const PAGE_PARAMS = ['agent_type_id', 'state', 'limit', 'cursor'] as const;

/** Copies the page params from a BFF request; `agentTypeId` replaces any agent_type_id given. */
export function pickRunsPageParams(
  incoming: URLSearchParams,
  agentTypeId?: string,
): URLSearchParams {
  const params = new URLSearchParams();
  for (const key of PAGE_PARAMS) {
    // Repeated keys join into one comma-separated value, the form orbit-agents reads.
    const value = incoming.getAll(key).filter(Boolean).join(',');
    if (value) params.set(key, value);
  }
  if (agentTypeId) params.set('agent_type_id', agentTypeId);
  return params;
}

/**
 * One page of runs with each starter's display name. Names are best-effort:
 * a principal that is not a tenant user gets none and the UI shows the raw id.
 */
export async function listRunsWithNames(
  accessToken: string,
  params: URLSearchParams,
): Promise<RunsPageResponse> {
  const page = await listRuns(accessToken, params);
  const names = await resolveUserDisplayNames(
    accessToken,
    page.runs.map((r) => r.created_by),
  );
  return {
    data: page.runs.map((r) => ({ ...r, created_by_name: names.get(r.created_by) ?? null })),
    next_cursor: page.next_cursor,
  };
}
