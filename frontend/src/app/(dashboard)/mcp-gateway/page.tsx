// RSC — the MCP servers list. Fetches the tool-gateway catalog and groups it
// into servers so the page renders with real data immediately.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchMcpCatalog } from './_services/mcp-server';
import { McpServersShell } from './_components/mcp-servers-shell';

export default async function McpServersPage() {
  const session = await getSession();
  const result = isSessionValid(session) ? await fetchMcpCatalog(session.accessToken) : null;

  const initialRows = result?.ok ? result.rows : [];
  const initialError = result && !result.ok ? result.message : null;

  return <McpServersShell initialRows={initialRows} initialError={initialError} />;
}
