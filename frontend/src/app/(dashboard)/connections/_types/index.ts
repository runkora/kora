// ── Display-layer types for the Connections feature ───────────────────────────
//
// Connections are read-only Connection custom resources (GitOps), surfaced by
// the egress gateway's GET /api/v1/connections. The UI never mutates them — it
// presents the connection catalog and computes, from the agent-type list, which
// agents attach each connection and through which actions.

import type { ConnectionCatalogEntry, AgentType } from '@orbit/shared/api';

/** An agent that attaches a connection, plus the actions it was granted through it. */
export interface ConnectionUsage {
  slug: string;
  name: string;
  /** The actions this agent may invoke through the connection (its connector's actions). */
  actions: string[];
}

export interface ConnectionRow {
  name: string;
  adapter: string;
  /** The connection's grantable action vocabulary (its adapter's declared actions). */
  actions: string[];
  /** Agents that attach this connection via one of their connectors. */
  usedBy: ConnectionUsage[];
}

// ── Builder ───────────────────────────────────────────────────────────────────

/**
 * Builds the display rows for the Connections page.
 *
 * For each connection in the catalog, `usedBy` is computed by scanning every
 * agent type's `connectors`: an agent uses a connection when one of its
 * connectors' `connection` matches the connection name. That connector's
 * `actions` are the actions granted to the agent through the connection.
 */
export function buildConnectionRows(
  catalog: ConnectionCatalogEntry[],
  agentTypes: AgentType[],
): ConnectionRow[] {
  return catalog.map((conn) => {
    const usedBy: ConnectionUsage[] = [];
    for (const agent of agentTypes) {
      const connector = (agent.connectors ?? []).find((c) => c.connection === conn.name);
      if (connector) {
        usedBy.push({ slug: agent.slug, name: agent.name, actions: connector.actions });
      }
    }
    return {
      name: conn.name,
      adapter: conn.adapter,
      actions: conn.actions,
      usedBy,
    };
  });
}

// ── Display helpers ───────────────────────────────────────────────────────────

const ADAPTER_LABELS: Record<string, string> = {
  github_app: 'GitHub App',
  passthrough: 'Passthrough',
};

/** Maps a raw adapter identifier to a human-readable label, falling back to the raw value. */
export function adapterLabel(adapter: string): string {
  return ADAPTER_LABELS[adapter] ?? adapter;
}
