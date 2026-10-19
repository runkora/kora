import 'server-only';
import { NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { listMcpServers, listMcpTools } from '@runkora/server/mcp-client';

// GET /api/agents/mcp-tools — the governed MCP tool catalog (servers + tools with
// their mcp:<server>:<tool>:use scopes), read from the tool-gateway. Powers the
// agent's fine-grained MCP tool-grant picker. Session forwarded by withAuth.
export const GET = withAuth(async (_req, session) => {
  try {
    const [servers, catalog] = await Promise.all([
      listMcpServers(session.accessToken),
      listMcpTools(session.accessToken),
    ]);
    return NextResponse.json({ servers, catalog });
  } catch {
    // Additive feature: if the tool-gateway is unreachable, degrade to an empty
    // catalog so the agent form still renders.
    return NextResponse.json({ servers: [], catalog: [] });
  }
});
