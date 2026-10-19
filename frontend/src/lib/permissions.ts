/**
 * Permission helpers for scope-based UI gating.
 *
 * `scopeCovers` / `hasScopeCovering` apply the platform's prefix-covers
 * hierarchy, mirroring the backend `scope_covers` rego rule and Go
 * `policy.ScopeCovers`: `agents:types:write` covers `agents:types:delegated:write`.
 * There is no exact-match helper. An exact match denies controls to a principal
 * holding a broader scope that the backend accepts. None of these evaluate
 * policy rules; per-resource answers come from the API (`can_run`, `can_fire`).
 */

/**
 * Returns `true` if the `granted` scope covers the `required` scope under the
 * kora scope hierarchy: same resource type (first segment), same action
 * (last segment), and the granted path (the middle segments) is a prefix of
 * the required path. Mirrors the backend `scope_covers` rego rule exactly —
 * keep the two in sync.
 */
export function scopeCovers(granted: string, required: string): boolean {
  if (granted === required) return true;
  const a = granted.split(':');
  const b = required.split(':');
  if (a.length < 2 || b.length < 2) return false;
  if (a[0] !== b[0]) return false;
  if (a[a.length - 1] !== b[b.length - 1]) return false;
  const pathA = a.slice(1, -1);
  const pathB = b.slice(1, -1);
  if (pathA.length > pathB.length) return false;
  return pathA.every((seg, i) => seg === pathB[i]);
}

/** Returns `true` if any scope in `scopes` covers the required scope. */
export function hasScopeCovering(scopes: string[], required: string): boolean {
  return scopes.some((s) => scopeCovers(s, required));
}
