/**
 * Permission helpers for scope-based UI gating.
 *
 * `hasScope` / `hasAnyScope` / `hasAllScopes` perform set membership checks
 * only. `scopeCovers` / `hasScopeCovering` additionally apply the platform's
 * prefix-covers hierarchy (mirroring the backend `scope_covers` rego rule and
 * Go `policy.ScopeCovers`), for required scopes that a broader granted scope
 * satisfies, e.g. `agents:types:write` covers `agents:types:delegated:write`.
 * The scope set is projected by the policy gateway's OPA evaluation and
 * cached in the session. None of these evaluate policy rules.
 */

/** Returns `true` if `scopes` contains the required scope. */
export function hasScope(scopes: string[], required: string): boolean {
  return scopes.includes(required);
}

/** Returns `true` if `scopes` contains at least one of the required scopes. */
export function hasAnyScope(scopes: string[], required: string[]): boolean {
  return required.some((s) => scopes.includes(s));
}

/** Returns `true` if `scopes` contains all of the required scopes. */
export function hasAllScopes(scopes: string[], required: string[]): boolean {
  return required.every((s) => scopes.includes(s));
}

/**
 * Returns `true` if the `granted` scope covers the `required` scope under the
 * Orbit scope hierarchy: same resource type (first segment), same action
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
