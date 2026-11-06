import 'server-only';

import { listSources, listTriggers } from '@runkora/server/triggers-client';
import { agentsClient } from '@runkora/server/agents-client';
import { ApiError } from '@runkora/server/client';
import { loadErrorMessage } from '@runkora/lib/load-error';
import type { AgentType } from '@runkora/shared/api';
import { toSourceRow, toTriggerRow } from '../_types';
import type { AgentOption, SourceRow, TriggerRow } from '../_types';
import { toTenantStatsRow } from '../_types/stats';
import type { TenantStatsRow } from '../_types/stats';

export type TriggersResult =
  | {
      ok: true;
      sources: SourceRow[];
      triggers: TriggerRow[];
      /** Null when the API answered without tenant stats. */
      stats: TenantStatsRow | null;
      agents: AgentOption[];
      /** Why the agent picker is empty, when the agents could not be read. */
      agentsError: string | null;
    }
  | { ok: false; message: string };

/**
 * What the page says when there is no usable session.
 *
 * Exported so the route renders the same sentence for a session it rejected
 * before calling, and this module for one the upstream rejected. An expired
 * token must never read as a tenant with no triggers.
 */
export const SESSION_EXPIRED_MESSAGE = 'Session expired. Sign in again.';

/**
 * Loads everything the platform Triggers page needs in one server pass: webhook
 * sources, triggers with their stats, and the tenant's agent types (for the
 * trigger form's agent picker). A failed agent read leaves the picker empty with
 * a reason rather than failing the whole page.
 */
export async function fetchTriggersPage(accessToken: string): Promise<TriggersResult> {
  try {
    const [sources, triggers] = await Promise.all([
      listSources(accessToken),
      listTriggers(accessToken),
    ]);
    const agents = await loadAgents(accessToken);
    return {
      ok: true,
      sources: sources.map(toSourceRow),
      triggers: triggers.triggers.map(toTriggerRow),
      stats: triggers.stats === null ? null : toTenantStatsRow(triggers.stats),
      agents: agents.ok ? agents.data : [],
      agentsError: agents.ok ? null : agents.message,
    };
  } catch (err) {
    // A rejected token, a refusal and an unreachable service are different
    // problems with different fixes. Each gets its own sentence.
    if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
      return { ok: false, message: loadErrorMessage(err.status, 'triggers') };
    }
    // Keep the upstream's own words: "trigger store unavailable" is worth far
    // more to whoever has to fix it than "failed to load".
    const detail = err instanceof ApiError ? err.message : null;
    return {
      ok: false,
      message: detail ? `Failed to load triggers: ${detail}` : 'Failed to load triggers.',
    };
  }
}

async function loadAgents(
  accessToken: string,
): Promise<{ ok: true; data: AgentOption[] } | { ok: false; message: string }> {
  try {
    const upstream = await agentsClient(accessToken).get<{ agent_types: AgentType[] }>(
      '/api/v1/agent-types',
    );
    return {
      ok: true,
      data: (upstream?.agent_types ?? []).map((a) => ({
        id: a.id,
        slug: a.slug,
        name: a.name,
        identityMode: a.identity_mode ?? 'autonomous',
        canRun: a.can_run ?? false,
      })),
    };
  } catch (err) {
    const status = err instanceof ApiError ? err.status : 500;
    return { ok: false, message: loadErrorMessage(status, 'agents') };
  }
}
