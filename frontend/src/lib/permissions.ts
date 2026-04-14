/**
 * Permission helpers for scope-based UI gating.
 *
 * These functions perform set membership checks only — they never interpret
 * scope hierarchy or evaluate policy rules. The scope set is projected by
 * the policy gateway's OPA evaluation and cached in the session.
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
