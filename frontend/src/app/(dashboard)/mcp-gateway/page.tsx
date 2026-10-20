// RSC — the MCP servers list. Fetches the tenant-visible servers from the
// tool-gateway so the page renders with real data immediately.

import { getSession, isSessionValid } from '@runkora/server/session';
import { fetchMcpServers } from './_services/mcp-server';
import { McpServersShell } from './_components/mcp-servers-shell';

export default async function McpServersPage() {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return <McpServersShell servers={[]} error="Not authenticated" />;
  }

  const result = await fetchMcpServers(session.accessToken);
  const servers = result.ok ? result.servers : [];
  const error = result.ok ? null : result.message;

  return <McpServersShell servers={servers} error={error} />;
}
