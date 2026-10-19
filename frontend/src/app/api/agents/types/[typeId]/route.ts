import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { agentsClient } from '@runkora/server/agents-client';
import { ApiError } from '@runkora/server/client';
import type { AgentType, UpdateAgentTypeRequest } from '@runkora/shared/api';

type Params = { typeId: string };

async function resolveAgentType(
  accessToken: string,
  tenantId: string,
  typeId: string,
): Promise<AgentType | null> {
  const client = agentsClient(accessToken);
  // Try direct UUID lookup first.
  try {
    const at = await client.get<AgentType>(`/api/v1/agent-types/${typeId}`);
    if (at) return at;
  } catch (err) {
    if (!(err instanceof ApiError) || err.status !== 404) throw err;
  }
  // Fall back to slug lookup via list.
  const list = await client.get<{ agent_types: AgentType[] }>(
    `/api/v1/agent-types?tenant_id=${encodeURIComponent(tenantId)}`,
  );
  return list?.agent_types.find((a) => a.slug === typeId) ?? null;
}

export const GET = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { typeId } = await (context?.params ?? Promise.resolve({ typeId: '' }));
    try {
      const at = await resolveAgentType(session.accessToken, session.tenantId, typeId);
      if (!at) {
        return NextResponse.json(
          { code: 'not_found', message: 'Agent type not found' },
          { status: 404 },
        );
      }
      return NextResponse.json({ data: at });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);

export const PATCH = withAuth(
  async (req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { typeId } = await (context?.params ?? Promise.resolve({ typeId: '' }));
    try {
      const at = await resolveAgentType(session.accessToken, session.tenantId, typeId);
      if (!at) {
        return NextResponse.json(
          { code: 'not_found', message: 'Agent type not found' },
          { status: 404 },
        );
      }
      const body = (await req.json()) as UpdateAgentTypeRequest;
      const client = agentsClient(session.accessToken);
      const updated = await client.patch<AgentType>(`/api/v1/agent-types/${at.id}`, body);
      return NextResponse.json({ data: updated });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);

export const DELETE = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { typeId } = await (context?.params ?? Promise.resolve({ typeId: '' }));
    try {
      const at = await resolveAgentType(session.accessToken, session.tenantId, typeId);
      if (!at) {
        return NextResponse.json(
          { code: 'not_found', message: 'Agent type not found' },
          { status: 404 },
        );
      }
      const client = agentsClient(session.accessToken);
      await client.delete(`/api/v1/agent-types/${at.id}`);
      return new NextResponse(null, { status: 204 });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);
