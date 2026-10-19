import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { fireTrigger } from '@runkora/server/triggers-client';
import type { TriggerFireRequest } from '@runkora/shared/api';

type Params = { id: string };

// POST /api/triggers/[id]/fire — fire a manual trigger (202).
export const POST = withAuth(
  async (req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { id } = await (context?.params ?? Promise.resolve({ id: '' }));
    const body = (await req.json().catch(() => ({}))) as TriggerFireRequest;
    const result = await fireTrigger(session.accessToken, id, body);
    return NextResponse.json({ data: result }, { status: 202 });
  },
);
