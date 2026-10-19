import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { agentsClient } from '@runkora/server/agents-client';
import { ApiError } from '@runkora/server/client';
import type { CreateAgentTypeRequest } from '@runkora/shared/api';

export const GET = withAuth(async (_req, session) => {
  try {
    const client = agentsClient(session.accessToken);
    const upstream = await client.get<{ agent_types: unknown[] }>(
      `/api/v1/agent-types?tenant_id=${encodeURIComponent(session.tenantId)}`,
    );
    return NextResponse.json({ data: upstream?.agent_types ?? [] });
  } catch (err) {
    if (err instanceof ApiError) {
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
    }
    throw err;
  }
});

export const POST = withAuth(async (req: NextRequest, session) => {
  const body = (await req.json()) as CreateAgentTypeRequest;
  if (!body.name || !body.slug) {
    return NextResponse.json(
      { code: 'validation_error', message: 'name and slug are required' },
      { status: 400 },
    );
  }
  try {
    const client = agentsClient(session.accessToken);
    const created = await client.post<unknown>('/api/v1/agent-types', {
      ...body,
      tenant_id: session.tenantId,
    });
    return NextResponse.json({ data: created }, { status: 201 });
  } catch (err) {
    if (err instanceof ApiError) {
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
    }
    throw err;
  }
});
