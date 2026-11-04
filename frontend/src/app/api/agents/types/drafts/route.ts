import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { agentsClient } from '@runkora/server/agents-client';
import { ApiError } from '@runkora/server/client';

/**
 * Agent type drafts — the builder's persistence layer.
 *
 * A draft is a saved form, not an agent type: it holds no grant, provisions
 * nothing, and is private to the caller (the backend filters every read and
 * write to `created_by` from the verified claims). See
 * docs/adr/2026-07-17-agent-builder.md §2.
 */

export const GET = withAuth(async (_req, session) => {
  try {
    const client = agentsClient(session.accessToken);
    const upstream = await client.get<{ drafts: unknown[] }>('/api/v1/agent-type-drafts');
    return NextResponse.json({ data: upstream?.drafts ?? [] });
  } catch (err) {
    if (err instanceof ApiError) {
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
    }
    throw err;
  }
});

export const POST = withAuth(async (req: NextRequest, session) => {
  const body = (await req.json().catch(() => null)) as { payload?: unknown } | null;
  if (!body || typeof body.payload !== 'object' || body.payload === null) {
    return NextResponse.json(
      { code: 'validation_error', message: 'payload object is required' },
      { status: 400 },
    );
  }
  try {
    const client = agentsClient(session.accessToken);
    const created = await client.post<unknown>('/api/v1/agent-type-drafts', {
      payload: body.payload,
    });
    return NextResponse.json({ data: created }, { status: 201 });
  } catch (err) {
    if (err instanceof ApiError) {
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
    }
    throw err;
  }
});
