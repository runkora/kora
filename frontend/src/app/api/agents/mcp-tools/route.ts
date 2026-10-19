import 'server-only';
import { NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { ApiError } from '@runkora/server/client';
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
  } catch (err) {
    // A refusal is passed through so the picker says so. Anything else is
    // additive: if the upstream is unreachable, degrade to an empty
    // catalog so the agent form still renders.
    if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
    }
    return NextResponse.json({ servers: [], catalog: [] });
  }
});
