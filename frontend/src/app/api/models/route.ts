import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { fetchRawModelsFromGateway } from '../../(dashboard)/model-gateway/_services/models-server';
import { fetchEffectiveScopes } from '../../(dashboard)/policy/_services/policy-server';
import { filterModelsByScopes } from './_filter';

export const GET = withAuth(async (_req: NextRequest, session) => {
  const [modelsResult, scopesResult] = await Promise.all([
    fetchRawModelsFromGateway(session.accessToken),
    fetchEffectiveScopes(session.accessToken),
  ]);

  if (!modelsResult.ok) {
    console.error(
      `[GET /api/models] upstream error ${modelsResult.status}: ${modelsResult.message}`,
    );
    const clientStatus = modelsResult.status >= 500 ? 502 : modelsResult.status;
    return NextResponse.json(
      { code: 'upstream_error', message: 'Failed to load models from inference gateway' },
      { status: clientStatus },
    );
  }

  const effectiveScopes = scopesResult.ok ? scopesResult.data.scopes : [];
  const filtered = filterModelsByScopes(modelsResult.models, effectiveScopes);

  return NextResponse.json({ data: filtered });
});
