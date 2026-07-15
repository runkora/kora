import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { getDevEnvironment, destroyDevEnvironment } from '@orbit/server/devenvironments-client';
import { ApiError } from '@orbit/server/client';

type Params = { id: string };

// GET /api/dev-environments/[id] — poll for status (phase, appURL, expiresAt).
export const GET = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { id } = await (context?.params ?? Promise.resolve({ id: '' }));
    try {
      const environment = await getDevEnvironment(session.accessToken, id);
      return NextResponse.json({ data: environment });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);

// DELETE /api/dev-environments/[id] — destroy an environment (204). Also
// serves an admin force-destroy of someone else's environment — the
// devenvironments:admin bypass is decided server-side, not by this route.
export const DELETE = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { id } = await (context?.params ?? Promise.resolve({ id: '' }));
    try {
      await destroyDevEnvironment(session.accessToken, id);
      return new NextResponse(null, { status: 204 });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);
