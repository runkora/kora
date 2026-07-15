import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { config } from '@orbit/server/config';
import { withAuth } from '@orbit/server/route-helpers';
import { listDevEnvironments, createDevEnvironment } from '@orbit/server/devenvironments-client';
import { ApiError } from '@orbit/server/client';
import type { DevEnvironmentCreateRequest } from '@orbit/shared/api';

// GET /api/dev-environments — the caller's own ephemeral dev environments,
// plus the platform-wide limits (TTL hard cap, per-owner quota) the backend
// enforces, so the client never hardcodes them.
export const GET = withAuth(async (_req, session) => {
  try {
    const { environments, limits } = await listDevEnvironments(session.accessToken);
    return NextResponse.json({ data: environments, limits });
  } catch (err) {
    if (err instanceof ApiError) {
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
    }
    throw err;
  }
});

// POST /api/dev-environments — provision a new environment (201).
// Dev environments cannot themselves provision further dev environments —
// this install's own config.devEnvironmentId is set only when it is running
// inside one (see frontend/src/server/config.ts). This is UX only: the real
// enforcement is server-side in internal/devenvironments's CreateEnvironment
// (ErrNestedProvisioning -> 403 devenvironments_nested_forbidden), which
// checks before quota, before any write. This BFF-side check just fails
// fast with a clear message before round-tripping to the backend.
export const POST = withAuth(async (req: NextRequest, session) => {
  if (config.devEnvironmentId) {
    return NextResponse.json(
      { code: 'devenvironments_nested_forbidden', message: 'Dev environments cannot create further dev environments.' },
      { status: 403 },
    );
  }

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
