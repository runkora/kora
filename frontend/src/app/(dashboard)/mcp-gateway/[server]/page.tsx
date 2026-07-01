// RSC — one MCP server's tools. Fetches the catalog and filters to this server.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchMcpCatalog } from '../_services/mcp-server';
import { McpServerDetailShell } from '../_components/mcp-server-detail-shell';

export default async function McpServerDetail({ params }: { params: Promise<{ server: string }> }) {
  const { server } = await params;
  const session = await getSession();
  const result = isSessionValid(session) ? await fetchMcpCatalog(session.accessToken) : null;

  const initialRows = result?.ok ? result.rows : [];
  const initialError = result && !result.ok ? result.message : null;

  return (
    <McpServerDetailShell
      serverSlug={server}
      initialRows={initialRows}
      initialError={initialError}
    />
  );
}
