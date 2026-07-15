import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { extendDevEnvironment } from '@orbit/server/devenvironments-client';
import { ApiError } from '@orbit/server/client';
import type { DevEnvironmentExtendRequest } from '@orbit/shared/api';

type Params = { id: string };

// POST /api/dev-environments/[id]/extend — raise an environment's TTL.
export const POST = withAuth(
  async (req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { id } = await (context?.params ?? Promise.resolve({ id: '' }));
    const body = (await req.json().catch(() => ({}))) as DevEnvironmentExtendRequest;
    if (!body.ttl?.trim()) {
      return NextResponse.json(
        { code: 'validation_error', message: 'ttl is required' },
        { status: 400 },
      );
    }
    try {
      const environment = await extendDevEnvironment(session.accessToken, id, { ttl: body.ttl });
      return NextResponse.json({ data: environment });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);
