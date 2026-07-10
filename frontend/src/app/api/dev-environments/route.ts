import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { listDevEnvironments, createDevEnvironment } from '@orbit/server/devenvironments-client';
import { ApiError } from '@orbit/server/client';
import type { DevEnvironmentCreateRequest } from '@orbit/shared/api';

// GET /api/dev-environments — the caller's own ephemeral dev environments.
export const GET = withAuth(async (_req, session) => {
  try {
    const environments = await listDevEnvironments(session.accessToken);
    return NextResponse.json({ data: environments });
  } catch (err) {
    if (err instanceof ApiError) {
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
    }
    throw err;
  }
});

// POST /api/dev-environments — provision a new environment (201).
export const POST = withAuth(async (req: NextRequest, session) => {
  const body = (await req.json().catch(() => ({}))) as DevEnvironmentCreateRequest;
  try {
    const created = await createDevEnvironment(session.accessToken, { ref: body.ref });
    return NextResponse.json({ data: created }, { status: 201 });
  } catch (err) {
    if (err instanceof ApiError) {
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
    }
    throw err;
  }
});
