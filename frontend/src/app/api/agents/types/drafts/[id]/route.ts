import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { agentsClient } from '@runkora/server/agents-client';
import { ApiError } from '@runkora/server/client';

/**
 * Single agent type draft — get, update (full payload replace), delete.
 *
 * The backend returns not-found for another caller's draft, so ownership
 * never surfaces here. See docs/adr/2026-07-17-agent-builder.md §2.
 */

type Params = { id: string };

export const GET = withAuth<Params>(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { id } = await (context?.params ?? Promise.resolve({ id: '' }));
    try {
      const client = agentsClient(session.accessToken);
      const draft = await client.get<unknown>(
        `/api/v1/agent-type-drafts/${encodeURIComponent(id)}`,
      );
      return NextResponse.json({ data: draft });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);

export const PATCH = withAuth<Params>(
  async (req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { id } = await (context?.params ?? Promise.resolve({ id: '' }));
    const body = (await req.json().catch(() => null)) as { payload?: unknown } | null;
    if (!body || typeof body.payload !== 'object' || body.payload === null) {
      return NextResponse.json(
        { code: 'validation_error', message: 'payload object is required' },
        { status: 400 },
      );
    }
    try {
      const client = agentsClient(session.accessToken);
      const updated = await client.patch<unknown>(
        `/api/v1/agent-type-drafts/${encodeURIComponent(id)}`,
        { payload: body.payload },
      );
      return NextResponse.json({ data: updated });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);

export const DELETE = withAuth<Params>(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { id } = await (context?.params ?? Promise.resolve({ id: '' }));
    try {
      const client = agentsClient(session.accessToken);
      await client.delete<unknown>(`/api/v1/agent-type-drafts/${encodeURIComponent(id)}`);
      return NextResponse.json({ data: { deleted: true } });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);
