import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { previewEffectiveAccess } from '@orbit/server/agents-client';
import { ApiError } from '@orbit/server/client';
import type { EffectiveAccessPreviewRequest } from '@orbit/shared/api';

// POST /api/agents/types/effective-access — resolves the effective-access view
// for a *draft* (unsaved) connectivity config. Backs the Connectivity tab's
// live preview; nothing is persisted. Mirrors the sibling effective-access
// routes but takes the draft in the request body rather than resolving by id.
export const POST = withAuth(async (req: NextRequest, session) => {
  const body = (await req.json()) as EffectiveAccessPreviewRequest;
  try {
    const access = await previewEffectiveAccess(session.accessToken, body);
    return NextResponse.json({ data: access });
  } catch (err) {
    if (err instanceof ApiError) {
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
    }
    throw err;
  }
});
