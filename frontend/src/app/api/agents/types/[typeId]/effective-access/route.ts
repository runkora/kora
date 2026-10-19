import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { agentsClient, fetchEffectiveAccess } from '@runkora/server/agents-client';
import { ApiError } from '@runkora/server/client';
import type { AgentType } from '@runkora/shared/api';

type Params = { typeId: string };

// Resolve the agent type by UUID first, then fall back to a slug lookup — the
// detail page may pass either. Mirrors ../route.ts.
async function resolveAgentType(
  accessToken: string,
  tenantId: string,
  typeId: string,
): Promise<AgentType | null> {
  const client = agentsClient(accessToken);
  try {
    const at = await client.get<AgentType>(`/api/v1/agent-types/${typeId}`);
    if (at) return at;
  } catch (err) {
    if (!(err instanceof ApiError) || err.status !== 404) throw err;
  }
  const list = await client.get<{ agent_types: AgentType[] }>(
    `/api/v1/agent-types?tenant_id=${encodeURIComponent(tenantId)}`,
  );
  return list?.agent_types.find((a) => a.slug === typeId) ?? null;
}

// GET /api/agents/types/[typeId]/effective-access — the resolved, read-only view
// of what the agent can actually reach (own egress + environment + connectors).
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
      const access = await fetchEffectiveAccess(session.accessToken, at.id);
      return NextResponse.json({ data: access });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);
