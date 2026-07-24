import 'server-only';

import { listSources, listTriggers, listDeliveries } from '@orbit/server/triggers-client';
import { agentsClient } from '@orbit/server/agents-client';
import type { AgentType } from '@orbit/shared/api';
import { toSourceRow, toTriggerRow, toDeliveryRow } from '../_types';
import type { AgentOption, DeliveryRow, SourceRow, TriggerRow } from '../_types';

export type TriggersResult =
  | {
      ok: true;
      sources: SourceRow[];
      triggers: TriggerRow[];
      deliveries: DeliveryRow[];
      agents: AgentOption[];
    }
  | { ok: false; message: string };

/**
 * Loads everything the platform Triggers page needs in one server pass: webhook
 * sources, triggers, recent deliveries, and the tenant's agent types (for the
 * trigger form's agent picker). Agent types degrade to an empty list if the
 * agent control plane is unreachable — the picker simply shows no options rather
 * than failing the whole page.
 */
export async function fetchTriggersPage(accessToken: string): Promise<TriggersResult> {
  try {
    const [sources, triggers, deliveries] = await Promise.all([
      listSources(accessToken),
      listTriggers(accessToken),
      listDeliveries(accessToken, { limit: 100 }),
    ]);
    return {
      ok: true,
      sources: sources.map(toSourceRow),
      triggers: triggers.map(toTriggerRow),
      deliveries: deliveries.map(toDeliveryRow),
      agents: await loadAgents(accessToken),
    };
  } catch {
    return { ok: false, message: 'Failed to load triggers' };
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
