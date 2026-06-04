import 'server-only';
import { NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { agentsClient } from '@orbit/server/agents-client';
import { ApiError } from '@orbit/server/client';
import type { ToolScopeDefinition } from '@orbit/shared/api';

export const GET = withAuth(async (_req, session) => {
  try {
    const client = agentsClient(session.accessToken);
    const result = await client.get<{ tool_scopes: ToolScopeDefinition[] }>(
      '/api/v1/agent-types/tool-scopes',
    );
    return NextResponse.json({ data: result?.tool_scopes ?? [] });
  } catch (err) {
    if (err instanceof ApiError) {
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
    }
    throw err;
  }
});
