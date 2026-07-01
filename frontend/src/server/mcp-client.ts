import 'server-only';
import { config } from '@orbit/server/config';
import type { McpCatalogTool } from '@orbit/shared/api';

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
    throw new Error(`tool-gateway list catalog failed: ${res.status}`);
  }
  const body = (await res.json()) as { tools?: McpCatalogTool[] };
  return body.tools ?? [];
}
