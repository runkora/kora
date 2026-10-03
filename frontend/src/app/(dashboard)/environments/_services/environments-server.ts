import 'server-only';

import { listEnvironments, listSandboxImages } from '@orbit/server/environments-client';
import { listConnections } from '@orbit/server/connections-client';
import { toEnvironmentRow } from '../_types';
import type { EnvironmentRow } from '../_types';
import type { ConnectionCatalogEntry, SandboxImage } from '@orbit/shared/api';

export type EnvironmentsResult =
  | {
      ok: true;
      rows: EnvironmentRow[];
      connections: ConnectionCatalogEntry[];
      /** Registered sandbox images, or null when the list did not load. */
      images: SandboxImage[] | null;
    }
  | { ok: false; message: string };

/**
 * Loads the tenant's environments, the connection catalog and the registered
 * sandbox images from orbit-agents. Only a failure to load environments fails
 * the page; the form works without the other two.
 */
export async function fetchEnvironments(accessToken: string): Promise<EnvironmentsResult> {
  try {
    const [environments, connections, images] = await Promise.all([
      listEnvironments(accessToken),
      loadCatalog(accessToken),
      listSandboxImages(accessToken).catch(() => null),
    ]);
    return { ok: true, rows: environments.map(toEnvironmentRow), connections, images };
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
