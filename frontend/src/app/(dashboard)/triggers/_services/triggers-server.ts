import 'server-only';

import { listSources, listTriggers, listDeliveries } from '@orbit/server/triggers-client';
import { agentsClient } from '@orbit/server/agents-client';
import { ApiError } from '@orbit/server/client';
import type { AgentType } from '@orbit/shared/api';
import { toSourceRow, toTriggerRow, toDeliveryRow, DELIVERY_PAGE_SIZE } from '../_types';
import type { AgentOption, DeliveryRow, SourceRow, TriggerRow } from '../_types';

export type TriggersResult =
  | {
      ok: true;
      sources: SourceRow[];
      triggers: TriggerRow[];
      deliveries: DeliveryRow[];
      deliveryCursor: string | null;
      agents: AgentOption[];
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
 * sources, triggers, recent deliveries, and the tenant's agent types (for the
 * trigger form's agent picker). Agent types degrade to an empty list if the
 * agent control plane is unreachable — the picker simply shows no options rather
 * than failing the whole page.
 */
export async function fetchTriggersPage(accessToken: string): Promise<TriggersResult> {
  try {
    const [sources, triggers, deliveryPage] = await Promise.all([
      listSources(accessToken),
      listTriggers(accessToken),
      // The grid reads payloadExcerpt; the full body is never rendered, so
      // asking for it costs a hundred webhook bodies a page for nothing.
      listDeliveries(accessToken, { limit: DELIVERY_PAGE_SIZE, omitPayload: true }),
    ]);
    return {
      ok: true,
      sources: sources.map(toSourceRow),
      triggers: triggers.map(toTriggerRow),
      deliveries: deliveryPage.deliveries.map(toDeliveryRow),
      deliveryCursor: deliveryPage.nextCursor,
      agents: await loadAgents(accessToken),
    };
  } catch (err) {
    // A rejected token and an unreachable service are different problems with
    // different fixes, and collapsing them left an expired session rendering as
    // a healthy page with nothing on it.
    if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
      return { ok: false, message: SESSION_EXPIRED_MESSAGE };
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

async function loadAgents(accessToken: string): Promise<AgentOption[]> {
  try {
    const upstream = await agentsClient(accessToken).get<{ agent_types: AgentType[] }>(
      '/api/v1/agent-types',
    );
    return (upstream?.agent_types ?? []).map((a) => ({
      id: a.id,
      slug: a.slug,
      name: a.name,
      identityMode: a.identity_mode ?? 'autonomous',
    }));
  } catch {
    return [];
  }
}
