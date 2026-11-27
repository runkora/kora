import 'server-only';

import { listRuns } from '@runkora/server/agents-client';
import { resolveUserDisplayNames } from '@runkora/server/services/identity-server';
import type { RunsPageResponse } from '@runkora/shared/api';

/** The run list filters the list and the live stream share; kora-agents validates them. */
const FILTER_PARAMS = ['agent_type_id', 'state', 'created_by', 'trigger_kind', 'q'] as const;

/** The run list params a BFF route passes through to kora-agents. */
const PAGE_PARAMS = [...FILTER_PARAMS, 'limit', 'cursor', 'grader', 'question', 'verdict'] as const;

function pick(incoming: URLSearchParams, keys: readonly string[]): URLSearchParams {
  const params = new URLSearchParams();
  for (const key of keys) {
    // Repeated keys join into one comma-separated value, the form kora-agents reads.
    const value = incoming.getAll(key).filter(Boolean).join(',');
    if (value) params.set(key, value);
  }
  return params;
}

/** Copies the page params from a BFF request; `agentTypeId` replaces any agent_type_id given. */
export function pickRunsPageParams(
  incoming: URLSearchParams,
  agentTypeId?: string,
): URLSearchParams {
  const params = pick(incoming, PAGE_PARAMS);
  if (agentTypeId) params.set('agent_type_id', agentTypeId);
  return params;
}

/** Copies the live stream's filters from a BFF request; the stream takes no page or grade params. */
export const pickRunsStreamParams = (incoming: URLSearchParams) => pick(incoming, FILTER_PARAMS);

/**
 * One page of runs with each starter's display name. Names are best-effort:
 * a principal that is not a tenant user gets none and the UI shows the raw id.
 */
export async function listRunsWithNames(
  accessToken: string,
  params: URLSearchParams,
  signal?: AbortSignal,
): Promise<RunsPageResponse> {
  const { runs, next_cursor } = (await listRuns(accessToken, params, signal)) ?? {
    runs: [],
    next_cursor: null,
  };
  signal?.throwIfAborted();
  const names = await resolveUserDisplayNames(
    accessToken,
    runs.filter((r) => !r.created_by_name).map((r) => r.created_by),
  );
  return {
    data: runs.map((r) => ({
      ...r,
      created_by_name: r.created_by_name ?? names.get(r.created_by) ?? null,
    })),
    next_cursor,
  };
}
