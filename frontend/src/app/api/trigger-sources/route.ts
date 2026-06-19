import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { listSources, createSource } from '@orbit/server/triggers-client';
import { ApiError } from '@orbit/server/client';
import type { WebhookSourceCreateRequest } from '@orbit/shared/api';

// GET /api/trigger-sources — the tenant's webhook sources.
export const GET = withAuth(async (_req, session) => {
  try {
    const sources = await listSources(session.accessToken);
    return NextResponse.json({ data: sources });
  } catch (err) {
    if (err instanceof ApiError) {
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
    }
    throw err;
  }
});

// POST /api/trigger-sources — register a webhook source (201). The response
// carries the one-time signing secret and webhook path.
export const POST = withAuth(async (req: NextRequest, session) => {
  const body = (await req.json()) as WebhookSourceCreateRequest;
  if (!body.name?.trim()) {
    return NextResponse.json(
      { code: 'validation_error', message: 'name is required' },
      { status: 400 },
    );
  }
  if (!body.provider?.trim()) {
    return NextResponse.json(
      { code: 'validation_error', message: 'provider is required' },
      { status: 400 },
    );
  }
  try {
    const created = await createSource(session.accessToken, {
      name: body.name,
      provider: body.provider,
      config: body.config ?? {},
    });
    return NextResponse.json({ data: created }, { status: 201 });
  } catch (err) {
    if (err instanceof ApiError) {
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
    }
    throw err;
  }
});
