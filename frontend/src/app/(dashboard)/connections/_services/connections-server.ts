import 'server-only';

import type { AgentType } from '@orbit/shared/api';
import { agentsClient } from '@orbit/server/agents-client';
import { ApiError } from '@orbit/server/client';
import { listConnections } from '@orbit/server/connections-client';
import { buildConnectionRows } from '../_types';
import type { ConnectionRow } from '../_types';

export type ConnectionsResult =
  { ok: true; rows: ConnectionRow[] } | { ok: false; message: string };

/**
 * Loads the read-only connection catalog and computes, for each connection,
 * which agents attach it and through which actions.
 *
 * Two upstreams:
 *   - the egress gateway's connection catalog (via `listConnections`), and
 *   - the orbit-agents agent-type list (via `agentsClient`, the same client
 *     the /agents page uses) to derive `usedBy`.
 *
 * Degradation: the connection catalog degrades to an empty list when the
 * gateway is unreachable (mirroring the picker route, which tolerates the same
 * gateway being down). The agent-type list is best-effort — if it fails, rows
 * are still returned with empty `usedBy` rather than failing the whole page.
 * Only an unexpected (non-ApiError) failure surfaces `ok: false`.
 */
export async function fetchConnections(
  accessToken: string,
  tenantId: string,
): Promise<ConnectionsResult> {
  try {
    const catalog = await loadCatalog(accessToken);
    const agentTypes = await loadAgentTypes(accessToken, tenantId);
    return { ok: true, rows: buildConnectionRows(catalog, agentTypes) };
  } catch {
    return { ok: false, message: 'Failed to load connections' };
  }
}

// The gateway owning connections may be unreachable in a partial deployment; the
// picker route already treats that as "no connections", so we mirror it here and
// degrade to an empty catalog instead of failing the page.
async function loadCatalog(accessToken: string) {
  try {
    return await listConnections(accessToken);
  } catch {
    return [];
  }
}

// usedBy is derived data — if the agent-type list is unavailable we still render
// the catalog with empty usage rather than blanking the page.
async function loadAgentTypes(accessToken: string, tenantId: string): Promise<AgentType[]> {
  try {
    const client = agentsClient(accessToken);
    const upstream = await client.get<{ agent_types: AgentType[] }>(
      `/api/v1/agent-types?tenant_id=${encodeURIComponent(tenantId)}`,
    );
    return upstream?.agent_types ?? [];
  } catch (err) {
    if (err instanceof ApiError) {
      return [];
    }
    throw err;
  }
}
