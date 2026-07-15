import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { kubeconfigDevEnvironment } from '@orbit/server/devenvironments-client';
import { ApiError } from '@orbit/server/client';

type Params = { id: string };

// POST /api/dev-environments/[id]/kubeconfig — broker a short-lived
// kubeconfig. The private key never leaves the identity service; this
// response carries the assembled kubeconfig once.
export const POST = withAuth(
  async (_req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { id } = await (context?.params ?? Promise.resolve({ id: '' }));
    try {
      const kubeconfig = await kubeconfigDevEnvironment(session.accessToken, id);
      return NextResponse.json({ data: kubeconfig });
    } catch (err) {
      if (err instanceof ApiError) {
        return NextResponse.json({ code: err.code, message: err.message }, { status: err.status });
      }
      throw err;
    }
  },
);
