import 'server-only';

import type { AgentType } from '@runkora/shared/api';
import { agentsClient } from '@runkora/server/agents-client';
import { ApiError } from '@runkora/server/client';
import { loadErrorMessage } from '@runkora/lib/load-error';
import { listConnections } from '@runkora/server/connections-client';
import { buildConnectionRows } from '../_types';
import type { ConnectionRow } from '../_types';

export type ConnectionsResult =
  | {
      ok: true;
      rows: ConnectionRow[];
      /** Why "used by" is empty for every row, when the agents could not be read. */
      usedByError: string | null;
    }
  | { ok: false; message: string };

/**
 * Loads the read-only connection catalog and computes, for each connection,
 * which agents attach it and through which actions.
 *
 * Two upstreams:
 *   - the egress gateway's connection catalog (via `listConnections`), and
 *   - the kora-agents agent-type list (via `agentsClient`, the same client
 *     the /agents page uses) to derive `usedBy`.
 *
 * Degradation: the connection catalog degrades to an empty list when the
 * gateway is unreachable (mirroring the picker route, which tolerates the same
 * gateway being down). The agent-type list is best-effort — if it fails, rows
 * are still returned with empty `usedBy` rather than failing the whole page.
 * A refused catalog read surfaces `ok: false` with a no-access sentence, as does
 * an unexpected (non-ApiError) failure.
 */
export async function fetchConnections(
  accessToken: string,
  tenantId: string,
): Promise<ConnectionsResult> {
  try {
    const catalog = await loadCatalog(accessToken);
    const agentTypes = await loadAgentTypes(accessToken, tenantId);
    return {
      ok: true,
      rows: buildConnectionRows(catalog, agentTypes.ok ? agentTypes.data : []),
      usedByError: agentTypes.ok ? null : agentTypes.message,
    };
  } catch (err) {
    if (err instanceof ApiError) {
      return { ok: false, message: loadErrorMessage(err.status, 'connections') };
    }
    return { ok: false, message: 'Failed to load connections' };
  }
}

// The gateway owning connections may be unreachable in a partial deployment; the
// picker route already treats that as "no connections", so we mirror it here and
// degrade to an empty catalog instead of failing the page. A refusal is not an
// outage: it propagates so the page says the caller has no access.
async function loadCatalog(accessToken: string) {
  try {
    return await listConnections(accessToken);
  } catch (err) {
    if (err instanceof ApiError && (err.status === 401 || err.status === 403)) throw err;
    return [];
  }
}

// usedBy is derived data. If the agent-type list is unavailable the catalog
// still renders, with the reason usage is missing.
async function loadAgentTypes(
  accessToken: string,
  tenantId: string,
): Promise<{ ok: true; data: AgentType[] } | { ok: false; message: string }> {
  try {
    const client = agentsClient(accessToken);
    const upstream = await client.get<{ agent_types: AgentType[] }>(
      `/api/v1/agent-types?tenant_id=${encodeURIComponent(tenantId)}`,
    );
    return { ok: true, data: upstream?.agent_types ?? [] };
  } catch (err) {
    if (err instanceof ApiError) {
      return { ok: false, message: loadErrorMessage(err.status, 'agents') };
    }
    throw err;
  }
}
