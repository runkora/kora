import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { fetchModelsFromGateway } from '../../(dashboard)/model-gateway/_services/models-server';

export const GET = withAuth(async (_req: NextRequest, session) => {
  const result = await fetchModelsFromGateway(session.accessToken);

  if (!result.ok) {
    console.error(`[GET /api/models] upstream error ${result.status}: ${result.message}`);
    const clientStatus = result.status >= 500 ? 502 : result.status;
    return NextResponse.json(
      { code: 'upstream_error', message: 'Failed to load models from inference gateway' },
      { status: clientStatus },
    );
  }

  return NextResponse.json({ data: result.models });
});
