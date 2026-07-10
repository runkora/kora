import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { wakeDevEnvironment } from '@orbit/server/devenvironments-client';
import { ApiError } from '@orbit/server/client';

type Params = { id: string };

// POST /api/dev-environments/[id]/wake — explicit wake from sleep (decision 8).
export const POST = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { id } = await (context?.params ?? Promise.resolve({ id: '' }));
    try {
      const environment = await wakeDevEnvironment(session.accessToken, id);
      return NextResponse.json({ data: environment });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);
