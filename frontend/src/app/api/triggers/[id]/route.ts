import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { updateTrigger, deleteTrigger } from '@orbit/server/triggers-client';
import { ApiError } from '@orbit/server/client';
import type { TriggerUpdateRequest } from '@orbit/shared/api';

type Params = { id: string };

// PATCH /api/triggers/[id] — edit a trigger's mutable fields.
export const PATCH = withAuth(
  async (req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { id } = await (context?.params ?? Promise.resolve({ id: '' }));
    const body = (await req.json()) as TriggerUpdateRequest;
    try {
      const updated = await updateTrigger(session.accessToken, id, body);
      return NextResponse.json({ data: updated });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);

// DELETE /api/triggers/[id] — delete a trigger (204).
export const DELETE = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { id } = await (context?.params ?? Promise.resolve({ id: '' }));
    try {
      await deleteTrigger(session.accessToken, id);
      return new NextResponse(null, { status: 204 });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);
