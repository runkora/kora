import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { fetchRawModelsFromGateway } from '../../(dashboard)/model-gateway/_services/models-server';
import { fetchEffectiveScopes } from '../../(dashboard)/policy/_services/policy-server';

// Derive the per-model inference scope from a model ID (e.g. "anthropic/claude-sonnet-4-6"
// → "inference:anthropic:claude-sonnet-4-6:use"). Returns null for non-standard IDs.
function modelUseScope(modelId: string): string | null {
  const parts = modelId.split('/');
  if (parts.length !== 2 || !parts[0] || !parts[1]) return null;
  return `inference:${parts[0]}:${parts[1]}:use`;
}

function filterModelsByScopes<T extends { id: string }>(
  models: T[],
  effectiveScopes: string[],
): T[] {
  // Extract all inference model-use scopes the caller holds.
  const inferenceModelScopes = new Set(
    effectiveScopes.filter((s) => s.startsWith('inference:') && s.endsWith(':use')),
  );

  // If the caller has no model-specific inference scopes, treat as broad access —
  // all models pass through (e.g. admin PATs with only inference:invoke).
  if (inferenceModelScopes.size === 0) return models;

  return models.filter((m) => {
    const scope = modelUseScope(m.id);
    return scope !== null && inferenceModelScopes.has(scope);
  });
}

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
