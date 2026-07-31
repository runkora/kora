import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { updateSource, deleteSource } from '@orbit/server/triggers-client';
import type { WebhookSourceUpdateRequest } from '@orbit/shared/api';

type Params = { id: string };

// PATCH /api/trigger-sources/[id] — enable/disable or rotate the signing secret.
export const PATCH = withAuth(
  async (req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { id } = await (context?.params ?? Promise.resolve({ id: '' }));
    const body = (await req.json()) as WebhookSourceUpdateRequest;
    const updated = await updateSource(session.accessToken, id, {
      enabled: body.enabled,
      rotate_secret: body.rotate_secret,
    });
    return NextResponse.json({ data: updated });
  },
);

// DELETE /api/trigger-sources/[id] — remove a source and its triggers (204).
export const DELETE = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { id } = await (context?.params ?? Promise.resolve({ id: '' }));
    await deleteSource(session.accessToken, id);
    return new NextResponse(null, { status: 204 });
  },
);
