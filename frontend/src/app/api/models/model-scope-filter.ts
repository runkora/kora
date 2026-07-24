import { hasScopeCovering } from '@orbit/lib/permissions';

// Derive the per-model inference scope from a model ID (e.g. "anthropic/claude-sonnet-4-6"
// → "inference:anthropic:claude-sonnet-4-6:use"). Returns null for non-standard IDs.
export function modelUseScope(modelId: string): string | null {
  const parts = modelId.split('/');
  if (parts.length !== 2 || !parts[0] || !parts[1]) return null;
  return `inference:${parts[0]}:${parts[1]}:use`;
}

// filterModelsByScopes returns the models the caller may use for inference.
//
// A caller's inference model-use scopes may be exact (inference:openai:gpt-5-4:use),
// family-level (inference:openai:use), or the broad inference:use — and a broader
// scope COVERS a more specific one under the platform scope hierarchy. Match with
// the covers hierarchy (hasScopeCovering), not exact set membership, so a caller
// granted a family/broad scope still sees the models it can use.
//
// inference:invoke is a different action (it does not end in :use), so a caller
// with only inference:invoke holds no model-use scopes and gets broad access via
// the empty-set fallback below.
export function filterModelsByScopes<T extends { id: string }>(
  models: T[],
  effectiveScopes: string[],
): T[] {
  const inferenceUseScopes = effectiveScopes.filter(
    (s) => s.startsWith('inference:') && s.endsWith(':use'),
  );

  if (inferenceUseScopes.length === 0) return models;

  return models.filter((m) => {
    const required = modelUseScope(m.id);
    return required !== null && hasScopeCovering(inferenceUseScopes, required);
  });
}
