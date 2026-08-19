import type { InferenceUsageStatus, PrincipalIdentity } from '../_types';

/**
 * Maps an InferenceUsageStatus value to the Badge variant expected by Terra.
 *
 * - success           → 'healthy'  (green)
 * - policy_denied / auth_error → 'degraded' (yellow) — the request reached the
 *   gateway but was intentionally blocked by policy or auth; not a system fault
 * - everything else   → 'error'    (red) — provider errors, timeouts, routing
 *   failures that indicate an unintended failure
 */
export function getInferenceStatusVariant(
  status: InferenceUsageStatus,
): 'healthy' | 'degraded' | 'error' {
  switch (status) {
    case 'success':
      return 'healthy';
    case 'policy_denied':
    case 'auth_error':
    case 'canceled':
      return 'degraded';
    default:
      return 'error';
  }
}

/**
 * Converts an InferenceUsageStatus snake_case value to a human-readable label,
 * e.g. "policy_denied" → "Policy Denied". Only used within the inference logs
 * table; other parts of the app should not import this.
 */
export function fmtInferenceStatus(status: InferenceUsageStatus): string {
  return status.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
}

// ── Principal attribution ───────────────────────────────────────────────────
//
// Used by the admin Logs view ("All logs") to render the Principal column and
// the principal filter. The tenant/principal split comes from the verified JWT
// server-side; these helpers only shape how a principal is *shown*.

/**
 * Friendly label for a canonical principal type. Mirrors the token package
 * constants in backend/internal/platform/token/claims.go — HUMAN/AGENT/PAT/…
 * Unknown or empty types fall back to a generic "Principal" label.
 */
export function fmtPrincipalType(principalType: string | null | undefined): string {
  switch (principalType) {
    case 'HUMAN':
      return 'User';
    case 'AGENT':
      return 'Agent';
    case 'PAT':
      return 'Personal access token';
    case 'SERVICE':
      return 'Service';
    case 'SYSTEM':
      return 'System';
    default:
      return 'Principal';
  }
}

/** How a principal is shown in the Logs Principal column. */
export interface PrincipalDisplay {
  /** Human-friendly primary label: user name/email, agent slug, or the raw ID. */
  primary: string;
  /** Secondary line (email or raw ID); null when it would duplicate the primary. */
  secondary: string | null;
}

/**
 * Resolves how a principal is shown: humans via identity attribution
 * (display name, then email), agents via their SPIFFE `/agent/<slug>` suffix,
 * everything else falls back to the raw ID — always keeping the raw ID
 * reachable on the secondary line. Mirrors the budgets-page resolver so the two
 * admin surfaces label principals identically.
 */
export function resolvePrincipalDisplay(
  principalId: string,
  principalType: string | null | undefined,
  identity: PrincipalIdentity | null | undefined,
): PrincipalDisplay {
  if (principalType === 'AGENT') {
    const m = principalId.match(/\/agent\/([^/]+)$/);
    if (m) {
      return { primary: m[1], secondary: principalId };
    }
  }
  if (identity?.display_name || identity?.email) {
    return {
      primary: identity.display_name || identity.email || principalId,
      secondary: identity.email ?? principalId,
    };
  }
  return { primary: principalId, secondary: null };
}
