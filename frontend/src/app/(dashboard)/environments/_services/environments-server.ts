import 'server-only';

import { listEnvironments } from '@orbit/server/environments-client';
import { listConnections } from '@orbit/server/connections-client';
import { toEnvironmentRow } from '../_types';
import type { EnvironmentRow } from '../_types';
import type { ConnectionCatalogEntry } from '@orbit/shared/api';

export type EnvironmentsResult =
  | { ok: true; rows: EnvironmentRow[]; connections: ConnectionCatalogEntry[] }
  | { ok: false; message: string };

/**
 * Loads the tenant's environments and the connection catalog (for the
 * connectors picker in the create/edit form) directly from orbit-agents.
 *
 * The connection catalog degrades to an empty list when the gateway is
 * unreachable (mirroring the picker route, which tolerates the same gateway
 * being down). Only a failure to load environments surfaces `ok: false`.
 */
export async function fetchEnvironments(accessToken: string): Promise<EnvironmentsResult> {
  try {
    const environments = await listEnvironments(accessToken);
    const connections = await loadCatalog(accessToken);
    return { ok: true, rows: environments.map(toEnvironmentRow), connections };
  } catch {
    return { ok: false, message: 'Failed to load environments' };
  }
}

// The gateway owning connections may be unreachable in a partial deployment; the
// picker route already treats that as "no connections", so we mirror it here and
// degrade to an empty catalog instead of failing the page.
async function loadCatalog(accessToken: string): Promise<ConnectionCatalogEntry[]> {
  try {
    return await listConnections(accessToken);
  } catch {
    return [];
  }
}
