import 'server-only';
import { config } from '@runkora/server/config';
import type { McpCatalogTool, McpServer } from '@runkora/shared/api';
import { ApiError } from './api-error';

/**
 * Lists the MCP servers visible to the caller's tenant (its own + global),
 * with per-server metadata and tool counts, from the tool-gateway's
 * GET /v1/servers.
 */
export async function listMcpServers(accessToken: string): Promise<McpServer[]> {
  const res = await fetch(`${config.toolGatewayUrl}/v1/servers`, {
    headers: {
      Authorization: `Bearer ${accessToken}`,
      'Content-Type': 'application/json',
    },
    cache: 'no-store',
    signal: AbortSignal.timeout(15_000),
  });
  if (!res.ok) {
    throw new ApiError(
      res.status,
      'UPSTREAM_ERROR',
      `tool-gateway list servers failed: ${res.status}`,
    );
  }
  const body = (await res.json()) as { servers?: McpServer[] };
  return body.servers ?? [];
}

/**
 * Lists the caller's tenant's MCP tool catalog from the tool-gateway.
 *
 * The catalog is the gateway's read-only projection of every registered MCP
 * server's tools (rebuilt on each introspection). The tool-gateway is a plain
 * HTTP mesh service the BFF reaches directly (unlike the egress CONNECT broker);
 * we forward the session token and hit GET /v1/catalog. The result is scoped to
 * the caller's tenant (own + global servers) from the token, so no tenant param
 * is needed.
 */
export async function listMcpTools(accessToken: string): Promise<McpCatalogTool[]> {
  const res = await fetch(`${config.toolGatewayUrl}/v1/catalog`, {
    headers: {
      Authorization: `Bearer ${accessToken}`,
      'Content-Type': 'application/json',
    },
    cache: 'no-store',
    signal: AbortSignal.timeout(15_000),
  });
  if (!res.ok) {
    throw new ApiError(
      res.status,
      'UPSTREAM_ERROR',
      `tool-gateway list catalog failed: ${res.status}`,
    );
  }
  const body = (await res.json()) as { tools?: McpCatalogTool[] };
  return body.tools ?? [];
}

/**
 * The tool names the caller actually holds grants for on one server, via the
 * gateway's per-server tools/list (POST /v1/mcp/<server>) — the gateway filters
 * the list to the caller's grants. Used by the OAuth consent screen to show which
 * catalog tools are granted vs hidden. Returns [] on any failure (fail-closed:
 * the surface shows nothing granted rather than erroring the consent).
 */
export async function listGrantedTools(accessToken: string, server: string): Promise<string[]> {
  try {
    const res = await fetch(`${config.toolGatewayUrl}/v1/mcp/${encodeURIComponent(server)}`, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${accessToken}`,
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/list' }),
      signal: AbortSignal.timeout(15_000),
    });
    if (!res.ok) return [];
    const body = (await res.json()) as { result?: { tools?: { name: string }[] } };
    return (body.result?.tools ?? []).map((t) => t.name);
  } catch {
    return [];
  }
}
