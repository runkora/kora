import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { MOCK_AGENT_TYPES } from '../_mock/data';
import type { CreateAgentTypeRequest } from '@orbit/shared/api';

export const GET = withAuth(async () => {
  return NextResponse.json({ data: MOCK_AGENT_TYPES });
});

export const POST = withAuth(async (req: NextRequest) => {
  const body = (await req.json()) as CreateAgentTypeRequest;
  if (!body.name || !body.slug) {
    return NextResponse.json(
      { code: 'validation_error', message: 'name and slug are required' },
      { status: 400 },
    );
  }
  const created = {
    ...MOCK_AGENT_TYPES[0],
    id: `at_${Date.now()}`,
    name: body.name,
    slug: body.slug,
    description: body.description ?? '',
    system_prompt: body.system_prompt ?? '',
    framework: body.framework ?? ('claude-code' as const),
    tool_scopes: body.tool_scopes ?? [],
    grant_scopes: body.grant_scopes ?? [],
    max_concurrent_runs_per_pod: body.max_concurrent_runs_per_pod ?? 10,
    state: 'provisioning' as const,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  };
  return NextResponse.json({ data: created }, { status: 201 });
});
