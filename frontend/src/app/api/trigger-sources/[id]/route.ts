import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { updateSource, deleteSource } from '@orbit/server/triggers-client';
import { ApiError } from '@orbit/server/client';
import type { WebhookSourceUpdateRequest } from '@orbit/shared/api';

type Params = { id: string };

// PATCH /api/trigger-sources/[id] — enable/disable or rotate the signing secret.
export const PATCH = withAuth(
  async (req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { id } = await (context?.params ?? Promise.resolve({ id: '' }));
    const body = (await req.json()) as WebhookSourceUpdateRequest;
    try {
      const updated = await updateSource(session.accessToken, id, {
        enabled: body.enabled,
        rotate_secret: body.rotate_secret,
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

// DELETE /api/trigger-sources/[id] — remove a source and its triggers (204).
export const DELETE = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { id } = await (context?.params ?? Promise.resolve({ id: '' }));
    try {
      await deleteSource(session.accessToken, id);
      return new NextResponse(null, { status: 204 });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);
