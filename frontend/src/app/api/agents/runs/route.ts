import 'server-only';
import { NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { agentsClient } from '@orbit/server/agents-client';
import { ApiError } from '@orbit/server/client';

export const GET = withAuth(async (_req, session) => {
  try {
    const client = agentsClient(session.accessToken);
    const upstream = await client.get<{ runs: unknown[] }>(
      `/api/v1/runs?tenant_id=${encodeURIComponent(session.tenantId)}`,
    );
    return NextResponse.json({ data: upstream?.runs ?? [] });
  } catch (err) {
    if (err instanceof ApiError) {
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
    }
    throw err;
  }
});
