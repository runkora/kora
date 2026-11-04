import type { InferenceUsageStatus } from '../_types';

/**
 * Maps an InferenceUsageStatus value to the Badge variant expected by Hades.
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

/** Shown before the browser can say what host it is on, e.g. during SSR. */
export const GATEWAY_BASE_URL_FALLBACK = 'https://api.<your-kora-host>';

/**
 * Where agents send inference traffic, e.g. `https://api.example.com`.
 *
 * The Setup tab prints this into half a dozen config snippets (shell profile,
 * Codex TOML, OpenCode JSONC, VS Code settings, Continue). Deriving it from the
 * console's own host keeps every snippet correct on whichever cluster the user
 * is looking at — local, dev, or a customer's — instead of pinning one
 * environment. Inference is served on the API ingress (`api.<host>`), not the
 * console host, mirroring how `ingestHostFromWindow` derives the webhook host.
 */
export function gatewayBaseUrlFromWindow(): string {
  return typeof window === 'undefined'
    ? GATEWAY_BASE_URL_FALLBACK
    : `https://api.${window.location.host}`;
}
