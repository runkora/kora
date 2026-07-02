// RSC — the MCP servers list. Fetches the tenant-visible servers from the
// tool-gateway so the page renders with real data immediately.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchMcpServers } from './_services/mcp-server';
import { McpServersShell } from './_components/mcp-servers-shell';

export default async function McpServersPage() {
  const session = await getSession();
  const result = isSessionValid(session) ? await fetchMcpServers(session.accessToken) : null;

  const servers = result?.ok ? result.servers : [];
  const error = result && !result.ok ? result.message : null;

  return <McpServersShell servers={servers} error={error} />;
}
