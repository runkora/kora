// RSC — fetches the read-only MCP tool catalog from the tool-gateway so the
// page renders with real data immediately.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchMcpCatalog } from './_services/mcp-server';
import { McpGatewayShell } from './_components/mcp-gateway-shell';

export default async function McpGatewayPage() {
  const session = await getSession();
  const result = isSessionValid(session) ? await fetchMcpCatalog(session.accessToken) : null;

  const initialRows = result?.ok ? result.rows : [];
  const initialError = result && !result.ok ? result.message : null;

  return <McpGatewayShell initialRows={initialRows} initialError={initialError} />;
}
