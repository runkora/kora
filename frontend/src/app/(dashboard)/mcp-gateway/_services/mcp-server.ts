import 'server-only';

import type { McpCatalogTool, McpServer } from '@runkora/shared/api';
import { ApiError } from '@runkora/server/api-error';
import { listMcpServers, listMcpTools } from '@runkora/server/mcp-client';
import { loadErrorMessage } from '@runkora/lib/load-error';

export type McpServersResult = { ok: true; servers: McpServer[] } | { ok: false; message: string };

export type McpToolsResult =
  { ok: true; server: McpServer | null; tools: McpCatalogTool[] } | { ok: false; message: string };

/** Loads the MCP servers visible to the tenant for the top-level list. */
export async function fetchMcpServers(accessToken: string): Promise<McpServersResult> {
  try {
    return { ok: true, servers: await listMcpServers(accessToken) };
  } catch (err) {
    const status = err instanceof ApiError ? err.status : 0;
    return { ok: false, message: loadErrorMessage(status, 'MCP servers') };
  }
}

/**
 * Loads the data for one server's detail: its metadata (from the servers list)
 * and the full tool catalog (grouped by domain in the component).
 */
export async function fetchServerDetail(
  accessToken: string,
  serverSlug: string,
): Promise<McpToolsResult> {
  try {
    const [servers, tools] = await Promise.all([
      listMcpServers(accessToken),
      listMcpTools(accessToken),
    ]);
    return { ok: true, server: servers.find((s) => s.name === serverSlug) ?? null, tools };
  } catch {
    return { ok: false, message: 'Failed to load the MCP server' };
  }
}
