import type { InferenceUsageStatus } from '../_types';

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
