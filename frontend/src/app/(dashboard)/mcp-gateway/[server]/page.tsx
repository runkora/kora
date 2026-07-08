// RSC — one MCP server's detail. Fetches the server's metadata + tool catalog.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchServerDetail } from '../_services/mcp-server';
import { McpServerDetailShell } from '../_components/mcp-server-detail-shell';

export default async function McpServerDetail({ params }: { params: Promise<{ server: string }> }) {
  const { server } = await params;
  const session = await getSession();
  const result = isSessionValid(session)
    ? await fetchServerDetail(session.accessToken, server)
    : null;

  const serverMeta = result?.ok ? result.server : null;
  const tools = result?.ok ? result.tools : [];
  const error = result && !result.ok ? result.message : null;

  return (
    <McpServerDetailShell serverSlug={server} server={serverMeta} tools={tools} error={error} />
  );
}
