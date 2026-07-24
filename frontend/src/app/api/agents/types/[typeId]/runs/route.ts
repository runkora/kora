import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { agentsClient } from '@orbit/server/agents-client';
import { ApiError } from '@orbit/server/client';
import { resolveUserDisplayNames } from '@orbit/server/services/identity-server';
import type { AgentType, Run, RunWithAttribution } from '@orbit/shared/api';

type Params = { typeId: string };

async function resolveAgentTypeId(
  accessToken: string,
  tenantId: string,
  typeId: string,
): Promise<string | null> {
  const client = agentsClient(accessToken);
  try {
    const at = await client.get<AgentType>(`/api/v1/agent-types/${typeId}`);
    if (at) return at.id;
  } catch (err) {
    if (!(err instanceof ApiError) || err.status !== 404) throw err;
  }
  const list = await client.get<{ agent_types: AgentType[] }>(
    `/api/v1/agent-types?tenant_id=${encodeURIComponent(tenantId)}`,
  );
  return list?.agent_types.find((a) => a.slug === typeId)?.id ?? null;
}

export const GET = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { typeId } = await (context?.params ?? Promise.resolve({ typeId: '' }));
    try {
      const agentTypeId = await resolveAgentTypeId(session.accessToken, session.tenantId, typeId);
      if (!agentTypeId) {
        return NextResponse.json(
          { code: 'not_found', message: 'Agent type not found' },
          { status: 404 },
        );
      }
      const client = agentsClient(session.accessToken);
      const upstream = await client.get<{ runs: Run[] }>(
        `/api/v1/runs?tenant_id=${encodeURIComponent(session.tenantId)}&agent_type_id=${encodeURIComponent(agentTypeId)}`,
      );
      const runs = upstream?.runs ?? [];
      // Resolve created_by principals to display names (best-effort — principals
      // that are not tenant users get no name and the UI falls back to the raw id).
      const names = await resolveUserDisplayNames(
        session.accessToken,
        runs.map((r) => r.created_by),
      );
      const data: RunWithAttribution[] = runs.map((r) => ({
        ...r,
        created_by_name: names.get(r.created_by) ?? null,
      }));
      return NextResponse.json({ data });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);

export const POST = withAuth(
  async (req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { typeId } = await (context?.params ?? Promise.resolve({ typeId: '' }));
    try {
      const body = (await req.json().catch(() => ({}))) as {
        task?: string;
        provision_sandbox?: boolean;
      };
      const agentTypeId = await resolveAgentTypeId(session.accessToken, session.tenantId, typeId);
      if (!agentTypeId) {
        return NextResponse.json(
          { code: 'not_found', message: 'Agent type not found' },
          { status: 404 },
        );
      }
      const client = agentsClient(session.accessToken);
      // created_by is intentionally not sent — the backend derives the run creator
      // from the verified platform token, never from the request body.
      const run = await client.post<unknown>('/api/v1/runs', {
        tenant_id: session.tenantId,
        agent_type_id: agentTypeId,
        initial_message: body.task ?? '',
        provision_sandbox: body.provision_sandbox ?? false,
      });
      return NextResponse.json({ data: run }, { status: 201 });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);
