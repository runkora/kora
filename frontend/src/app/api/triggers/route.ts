import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { listTriggers, createTrigger } from '@orbit/server/triggers-client';
import type { TriggerCreateRequest } from '@orbit/shared/api';

function validateCreateRequest(body: TriggerCreateRequest): string | null {
  if (!body.name?.trim()) return 'name is required';
  if (!body.agent_type_id) return 'agent_type_id is required';
  if (!body.prompt_template?.trim()) return 'prompt_template is required';
  const kind = body.kind ?? 'webhook';
  switch (kind) {
    case 'webhook':
      // event_type is legal empty: it means every event the source emits.
      if (!body.source_id) {
        return 'source_id is required for webhook triggers';
      }
      return null;
    case 'scheduled':
      if (!body.schedule_config?.cron_expression) {
        return 'schedule_config.cron_expression is required for scheduled triggers';
      }
      return null;
    case 'manual':
      return null;
  }
}

// GET /api/triggers?agent_type_id= — triggers for the tenant, optionally filtered.
export const GET = withAuth(async (req: NextRequest, session) => {
  const agentTypeId = req.nextUrl.searchParams.get('agent_type_id') ?? undefined;
  const triggers = await listTriggers(session.accessToken, agentTypeId);
  return NextResponse.json({ data: triggers });
});

// POST /api/triggers — create a trigger (201). Required fields depend on
// kind: webhook needs source_id + event_type, scheduled needs
// schedule_config.cron_expression, manual needs neither.
export const POST = withAuth(async (req: NextRequest, session) => {
  const body = (await req.json()) as TriggerCreateRequest;
  const validationError = validateCreateRequest(body);
  if (validationError) {
    return NextResponse.json(
      { code: 'validation_error', message: validationError },
      { status: 400 },
    );
  }
  // agent_slug is intentionally omitted: the backend resolves the canonical
  // slug from agent_type_id and ignores any client-supplied value.
  const created = await createTrigger(session.accessToken, {
    kind: body.kind,
    source_id: body.source_id,
    name: body.name,
    agent_type_id: body.agent_type_id,
    event_type: body.event_type,
    match_condition: body.match_condition ?? {},
    prompt_template: body.prompt_template,
    schedule_config: body.schedule_config,
    manual_config: body.manual_config,
    sender_restriction: body.sender_restriction,
    max_parallel: body.max_parallel,
    overflow: body.overflow,
    max_queue_depth: body.max_queue_depth,
    timeout_seconds: body.timeout_seconds,
    dedupe_enabled: body.dedupe_enabled,
    autopause_enabled: body.autopause_enabled,
  });
  return NextResponse.json({ data: created }, { status: 201 });
});
