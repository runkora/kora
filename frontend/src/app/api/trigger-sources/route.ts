import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { listSources, createSource } from '@runkora/server/triggers-client';
import type { WebhookSourceCreateRequest } from '@runkora/shared/api';

// GET /api/trigger-sources — the tenant's webhook sources.
export const GET = withAuth(async (_req, session) => {
  const sources = await listSources(session.accessToken);
  return NextResponse.json({ data: sources });
});

// POST /api/trigger-sources — register a webhook source (201). The response
// carries the webhook path and, unless the provider issued the secret the
// request supplied, the one-time signing secret.
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
  const created = await createSource(session.accessToken, {
    name: body.name,
    provider: body.provider,
    config: body.config ?? {},
    ...(body.signing_secret !== undefined && { signing_secret: body.signing_secret }),
  });
  return NextResponse.json({ data: created }, { status: 201 });
});
