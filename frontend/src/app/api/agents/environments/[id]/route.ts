import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { updateEnvironment, deleteEnvironment } from '@orbit/server/environments-client';
import { ApiError } from '@orbit/server/client';
import type { EnvironmentRequest } from '@orbit/shared/api';

type Params = { id: string };

// PUT /api/agents/environments/[id] — full replace (200).
export const PUT = withAuth(
  async (req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { id } = await (context?.params ?? Promise.resolve({ id: '' }));
    const body = (await req.json()) as EnvironmentRequest;
    if (!body.name?.trim()) {
      return NextResponse.json(
        { code: 'validation_error', message: 'name is required' },
        { status: 400 },
      );
    }
    try {
      const updated = await updateEnvironment(session.accessToken, id, {
        name: body.name,
        description: body.description ?? '',
        sandbox_image: body.sandbox_image ?? '',
        size: body.size ?? { cpu: '', memory: '', disk: '' },
        egress_hosts: body.egress_hosts ?? [],
        connectors: body.connectors ?? [],
      });
      return NextResponse.json({ data: updated });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);

// DELETE /api/agents/environments/[id] — remove (204).
export const DELETE = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { id } = await (context?.params ?? Promise.resolve({ id: '' }));
    try {
      await deleteEnvironment(session.accessToken, id);
      return new NextResponse(null, { status: 204 });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);
