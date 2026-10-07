import 'server-only';

import {
  getEnvironmentSizes,
  listEnvironments,
  listSandboxImages,
} from '@orbit/server/environments-client';
import { listConnections } from '@orbit/server/connections-client';
import { toEnvironmentRow } from '../_types';
import type { EnvironmentRow } from '../_types';
import type { ConnectionCatalogEntry, EnvironmentSizes, SandboxImage } from '@orbit/shared/api';

export type EnvironmentsResult =
  | {
      ok: true;
      rows: EnvironmentRow[];
      connections: ConnectionCatalogEntry[];
      /** Registered sandbox images, or null when the list did not load. */
      images: SandboxImage[] | null;
      /** The platform's default and maximum sizes, or null when they did not load. */
      sizes: EnvironmentSizes | null;
    }
  | { ok: false; message: string };

/** Loads the Environments page: environments, connection catalog, sandbox images and size limits.
 * Only the environments are required; the form works without the rest. */
export async function fetchEnvironments(accessToken: string): Promise<EnvironmentsResult> {
  try {
    const [environments, connections, images, sizes] = await Promise.all([
      listEnvironments(accessToken),
      loadCatalog(accessToken),
      listSandboxImages(accessToken).catch(() => null),
      getEnvironmentSizes(accessToken).catch(() => null),
    ]);
    return { ok: true, rows: environments.map(toEnvironmentRow), connections, images, sizes };
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
