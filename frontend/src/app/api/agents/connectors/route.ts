import 'server-only';
import { NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { ApiError } from '@runkora/server/client';
import { listConnections } from '@runkora/server/connections-client';

// GET /api/agents/connectors — the catalog of the tenant's registered
// connections (name + grantable actions), read from orbit-agents (the service
// that serves the connection catalog over the mesh), session forwarded by withAuth.
export const GET = withAuth(async (_req, session) => {
  try {
    const connections = await listConnections(session.accessToken);
    return NextResponse.json({ data: connections });
  } catch (err) {
    // A refusal is passed through so the picker says so. Anything else is
    // additive: if the upstream is unreachable, degrade to an empty
    // catalog so the agent form still renders.
    if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
    }
    return NextResponse.json({ data: [] });
  }
});
