import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { fetchRawModelsFromGateway } from '../../(dashboard)/model-gateway/_services/models-server';
import { fetchEffectiveScopes } from '../../(dashboard)/policy/_services/policy-server';
import { filterModelsByScopes } from './model-scope-filter';

function gatewayErrorMessage(body: string): string {
  try {
    const parsed = JSON.parse(body) as { message?: unknown };
    if (typeof parsed.message === 'string') return parsed.message;
  } catch {
    // not JSON; fall through
  }
  return 'Invalid request';
}

export const GET = withAuth(async (req: NextRequest, session) => {
  // Optional, repeatable and/or comma-separated; the gateway validates the values.
  const capability = req.nextUrl.searchParams.getAll('capability');
  const [modelsResult, scopesResult] = await Promise.all([
    fetchRawModelsFromGateway(session.accessToken, capability),
    fetchEffectiveScopes(session.accessToken),
  ]);

  if (!modelsResult.ok) {
    console.error(
      `[GET /api/models] upstream error ${modelsResult.status}: ${modelsResult.message}`,
    );
    if (modelsResult.status === 400) {
      // A rejected capability value is the caller's mistake; the gateway names it.
      return NextResponse.json(
        { code: 'invalid_request', message: gatewayErrorMessage(modelsResult.message) },
        { status: 400 },
      );
    }
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
