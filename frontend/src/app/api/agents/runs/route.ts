import 'server-only';
import { NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { agentsClient } from '@orbit/server/agents-client';
import { ApiError } from '@orbit/server/client';
import { resolveUserDisplayNames } from '@orbit/server/services/identity-server';
import type { Run, RunWithAttribution } from '@orbit/shared/api';

export const GET = withAuth(async (req, session) => {
  try {
    const client = agentsClient(session.accessToken);
    const params = new URLSearchParams({ tenant_id: session.tenantId });
    // Forwarded rather than dropped: these narrow the row set upstream, and
    // every row it returns costs one policy evaluation there.
    const incoming = new URL(req.url).searchParams;
    for (const key of ['agent_type_id', 'limit'] as const) {
      const value = incoming.get(key);
      if (value) params.set(key, value);
    }
    const upstream = await client.get<{ runs: Run[] }>(`/api/v1/runs?${params.toString()}`);
    const runs = upstream?.runs ?? [];
    // Resolve created_by principals to display names (best-effort — principals
    // that are not tenant users get no name and the UI falls back to the raw id).
    const names = await resolveUserDisplayNames(
      session.accessToken,
      runs.map((r) => r.created_by),
    );
    const data: RunWithAttribution[] = runs.map((r) => ({
      ...r,
      created_by_name: names.get(r.created_by) ?? null,
    }));
    return NextResponse.json({ data });
  } catch (err) {
    if (err instanceof ApiError) {
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
    }
    throw err;
  }
});
