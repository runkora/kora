import 'server-only';
import { NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { listConnections } from '@orbit/server/connections-client';

// GET /api/agents/connectors — the catalog of the tenant's registered
// connections (name + grantable actions), read from orbit-agents (the service
// that serves the connection catalog over the mesh), session forwarded by withAuth.
export const GET = withAuth(async (_req, session) => {
  try {
    const connections = await listConnections(session.accessToken);
    return NextResponse.json({ data: connections });
  } catch {
    // Additive feature: if orbit-agents is unreachable, degrade to an empty
    // catalog so the agent form still renders.
    return NextResponse.json({ data: [] });
  }
});
