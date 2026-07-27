import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { fireTrigger } from '@orbit/server/triggers-client';
import { ApiError } from '@orbit/server/client';
import type { TriggerFireRequest } from '@orbit/shared/api';

type Params = { id: string };

// POST /api/triggers/[id]/fire — fire a manual trigger (202).
export const POST = withAuth(
  async (req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { id } = await (context?.params ?? Promise.resolve({ id: '' }));
    const body = (await req.json().catch(() => ({}))) as TriggerFireRequest;
    try {
      const result = await fireTrigger(session.accessToken, id, body);
      return NextResponse.json({ data: result }, { status: 202 });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);
