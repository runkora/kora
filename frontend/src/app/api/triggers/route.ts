import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { listTriggers, createTrigger } from '@orbit/server/triggers-client';
import { ApiError } from '@orbit/server/client';
import type { TriggerCreateRequest } from '@orbit/shared/api';

// GET /api/triggers?agent_type_id= — triggers for the tenant, optionally filtered.
export const GET = withAuth(async (req: NextRequest, session) => {
  const agentTypeId = req.nextUrl.searchParams.get('agent_type_id') ?? undefined;
  try {
    const triggers = await listTriggers(session.accessToken, agentTypeId);
    return NextResponse.json({ data: triggers });
  } catch (err) {
    if (err instanceof ApiError) {
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
    }
    throw err;
  }
});

// POST /api/triggers — create a trigger (201).
export const POST = withAuth(async (req: NextRequest, session) => {
  const body = (await req.json()) as TriggerCreateRequest;
  if (!body.name?.trim()) {
    return NextResponse.json(
      { code: 'validation_error', message: 'name is required' },
      { status: 400 },
    );
  }
  // agent_slug is intentionally not required: the backend resolves the canonical
  // slug from agent_type_id and ignores any client-supplied value.
  if (
    !body.source_id ||
    !body.agent_type_id ||
    !body.event_type ||
    !body.prompt_template?.trim()
  ) {
    return NextResponse.json(
      {
        code: 'validation_error',
        message: 'source_id, agent_type_id, event_type, and prompt_template are required',
      },
      { status: 400 },
    );
  }
  try {
    const created = await createTrigger(session.accessToken, {
      source_id: body.source_id,
      name: body.name,
      agent_type_id: body.agent_type_id,
      event_type: body.event_type,
      match_condition: body.match_condition ?? {},
      prompt_template: body.prompt_template,
    });
    return NextResponse.json({ data: created }, { status: 201 });
  } catch (err) {
    if (err instanceof ApiError) {
      return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
    }
    throw err;
  }
});
