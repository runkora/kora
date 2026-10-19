import 'server-only';
import { config } from '@runkora/server/config';
import type { ConnectionCatalogEntry, ConnectionQualifierEntry } from '@runkora/shared/api';

/** The slice of the gateway's connection record the catalog picker needs. */
interface GatewayConnection {
  name: string;
  adapter: string;
  actions?: string[];
  /** Action fragments the backend adds automatically at attachment. */
  baseline?: string[];
  /** Present only for adapters with a resource-qualifier dimension. */
  qualifier?: ConnectionQualifierEntry | null;
}

/**
 * Lists the caller's tenant's registered connections from orbit-agents.
 *
 * The read-only catalog is served by orbit-agents — a normal HTTP service the
 * frontend reaches through the mesh (it also serves agent-types) — rather than
 * the egress-gateway, which is a CONNECT/egress broker the frontend cannot reach
 * over plain HTTP. orbit-agents reads the same orbit-tenant-connections registry
 * the gateway brokers from. We forward the session token and hit the service
 * directly; the result is scoped to the caller's tenant from the token, so no
 * tenant param is needed. `actions` is the connection's adapter's declared
 * action vocabulary: the set of actions an agent may be granted through it.
 */
export async function listConnections(accessToken: string): Promise<ConnectionCatalogEntry[]> {
  const res = await fetch(`${config.koraAgentsUrl}/api/v1/connections`, {
    headers: {
      Authorization: `Bearer ${accessToken}`,
      'Content-Type': 'application/json',
    },
    cache: 'no-store',
    signal: AbortSignal.timeout(15_000),
  });
  if (!res.ok) {
    throw new Error(`orbit-agents list connections failed: ${res.status}`);
  }
  const body = (await res.json()) as { connections?: GatewayConnection[] };
  return (body.connections ?? []).map((c) => ({
    name: c.name,
    adapter: c.adapter,
    actions: c.actions ?? [],
    baseline: c.baseline ?? [],
    qualifier: c.qualifier ?? null,
  }));
}
