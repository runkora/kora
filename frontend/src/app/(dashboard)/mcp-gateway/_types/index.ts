import type { McpCatalogTool, McpServer } from '@orbit/shared/api';

/** @tier 3 — mcp-gateway-scoped models for the MCP catalog views. */

export type { McpServer };

/** One tool within a server, as shown in the per-domain grouped detail. */
export interface McpToolRow {
  /** Full tool name, e.g. "agents.create". */
  toolName: string;
  /** The name under its domain, e.g. "create" (or the full name if undomained). */
  leaf: string;
  /** The exact mcp:<server>:<tool>:use scope that grants this tool. */
  scope: string;
  description: string;
}

/** A domain (first path segment) and its tools, plus the domain rollup scope. */
export interface DomainGroup {
  /** e.g. "agents". */
  domain: string;
  /** mcp:<server>:<domain>:use — grants every tool in the domain. */
  rollupScope: string;
  tools: McpToolRow[];
}

/** Strips the mcp: prefix and :use suffix from a scope into path segments. */
function scopeSegments(scope: string): string[] {
  return scope.replace(/^mcp:/, '').replace(/:use$/, '').split(':');
}

/** The server-level rollup scope — grants every tool on the server. */
export function serverRollupScope(serverSlug: string): string {
  return `mcp:${serverSlug}:use`;
}

/** A compact relative-time label, e.g. "2h ago", from an RFC3339 timestamp. */
export function relativeTime(iso: string): string {
  if (!iso) return 'never';
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return 'never';
  const secs = Math.max(0, Math.floor((Date.now() - then) / 1000));
  if (secs < 60) return 'just now';
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
}

/** The dot colour for a server's health status (Terra status tokens). */
export function statusColor(status: string): string {
  if (status === 'healthy') return 'var(--color-status-healthy)';
  if (status === 'stale') return 'var(--color-status-degraded)';
  return 'var(--color-status-idle)';
}

/**
 * Groups a server's tools by domain (the first tool-name segment), attaching
 * the domain rollup scope, so the detail surfaces the server → domain → tool
 * grant hierarchy.
 */
export function buildDomainGroups(tools: McpCatalogTool[], serverSlug: string): DomainGroup[] {
  const byDomain = new Map<string, DomainGroup>();

  for (const t of tools) {
    if (t.server_slug !== serverSlug) continue;

    const segs = scopeSegments(t.scope); // [server, domain, …leaf]
    const server = segs[0] ?? serverSlug;
    const domain = segs[1] ?? t.tool_name;
    const dot = t.tool_name.indexOf('.');
    const leaf = dot >= 0 ? t.tool_name.slice(dot + 1) : t.tool_name;

    const row: McpToolRow = {
      toolName: t.tool_name,
      leaf,
      scope: t.scope,
      description: t.description,
    };
    const group = byDomain.get(domain);
    if (group) {
      group.tools.push(row);
    } else {
      byDomain.set(domain, { domain, rollupScope: `mcp:${server}:${domain}:use`, tools: [row] });
    }
  }

  const groups = [...byDomain.values()];
  for (const g of groups) g.tools.sort((a, b) => a.leaf.localeCompare(b.leaf));
  return groups.sort((a, b) => a.domain.localeCompare(b.domain));
}
