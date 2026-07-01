import type { McpCatalogTool } from '@orbit/shared/api';

/** @tier 3 — mcp-gateway-scoped row models for the catalog views. */

/** One tool of one server (the per-server detail table row). */
export interface McpToolRow {
  serverSlug: string;
  toolName: string;
  scope: string;
  description: string;
  /** true when the server is platform-global (tenant_id === ''). */
  global: boolean;
}

/** One MCP server (the top-level list row). */
export interface McpServerRow {
  serverSlug: string;
  global: boolean;
  toolCount: number;
}

/** Flattens the gateway catalog into per-tool rows. */
export function buildToolRows(tools: McpCatalogTool[]): McpToolRow[] {
  return tools.map((t) => ({
    serverSlug: t.server_slug,
    toolName: t.tool_name,
    scope: t.scope,
    description: t.description,
    global: t.tenant_id === '',
  }));
}

/** Groups tool rows into one summary row per server (sorted by name). */
export function buildServerRows(rows: McpToolRow[]): McpServerRow[] {
  const byServer = new Map<string, McpServerRow>();
  for (const r of rows) {
    const existing = byServer.get(r.serverSlug);
    if (existing) {
      existing.toolCount += 1;
    } else {
      byServer.set(r.serverSlug, { serverSlug: r.serverSlug, global: r.global, toolCount: 1 });
    }
  }
  return [...byServer.values()].sort((a, b) => a.serverSlug.localeCompare(b.serverSlug));
}

/** The tools belonging to one server, sorted by tool name. */
export function toolsForServer(rows: McpToolRow[], slug: string): McpToolRow[] {
  return rows
    .filter((r) => r.serverSlug === slug)
    .sort((a, b) => a.toolName.localeCompare(b.toolName));
}
