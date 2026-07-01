import 'server-only';

import { listMcpTools } from '@orbit/server/mcp-client';
import { buildMcpRows } from '../_types';
import type { McpToolRow } from '../_types';

export type McpCatalogResult = { ok: true; rows: McpToolRow[] } | { ok: false; message: string };

/**
 * Loads the read-only MCP tool catalog from the tool-gateway and flattens it
 * into table rows. Surfaces `ok: false` when the gateway is unreachable so the
 * page can show a clear message rather than a misleading empty catalog.
 */
export async function fetchMcpCatalog(accessToken: string): Promise<McpCatalogResult> {
  try {
    const tools = await listMcpTools(accessToken);
    return { ok: true, rows: buildMcpRows(tools) };
  } catch {
    return { ok: false, message: 'Failed to load the MCP tool catalog' };
  }
}
