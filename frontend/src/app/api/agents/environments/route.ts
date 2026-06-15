import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { listEnvironments, createEnvironment } from '@orbit/server/environments-client';
import { ApiError } from '@orbit/server/client';
import type { EnvironmentRequest } from '@orbit/shared/api';

// GET /api/agents/environments — the tenant's environments, read from
// orbit-agents over the mesh; session forwarded by withAuth.
export const GET = withAuth(async (_req, session) => {
  try {
    const environments = await listEnvironments(session.accessToken);
    return NextResponse.json({ data: environments });
  } catch (err) {
    if (err instanceof ApiError) {
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
    }
    throw err;
  }
});

// POST /api/agents/environments — create an environment (201).
export const POST = withAuth(async (req: NextRequest, session) => {
  const body = (await req.json()) as EnvironmentRequest;
  if (!body.name?.trim()) {
    return NextResponse.json(
      { code: 'validation_error', message: 'name is required' },
      { status: 400 },
    );
  }
  try {
    const created = await createEnvironment(session.accessToken, {
      name: body.name,
      description: body.description ?? '',
      egress_hosts: body.egress_hosts ?? [],
      connectors: body.connectors ?? [],
    });
    return NextResponse.json({ data: created }, { status: 201 });
  } catch (err) {
    if (err instanceof ApiError) {
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
    }
    throw err;
  }
});
