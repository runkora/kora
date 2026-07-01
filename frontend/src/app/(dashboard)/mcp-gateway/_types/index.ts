import type { McpCatalogTool } from '@orbit/shared/api';

/** @tier 3 — mcp-gateway-scoped row model for the catalog table. */
export interface McpToolRow {
  serverSlug: string;
  toolName: string;
  scope: string;
  description: string;
  /** true when the server is platform-global (tenant_id === ''). */
  global: boolean;
}

/** Flattens the gateway catalog into table rows. */
export function buildMcpRows(tools: McpCatalogTool[]): McpToolRow[] {
  return tools.map((t) => ({
    serverSlug: t.server_slug,
    toolName: t.tool_name,
    scope: t.scope,
    description: t.description,
    global: t.tenant_id === '',
  }));
}

/** Count of distinct servers in a row set (for the header summary). */
export function distinctServers(rows: McpToolRow[]): number {
  return new Set(rows.map((r) => r.serverSlug)).size;
}
