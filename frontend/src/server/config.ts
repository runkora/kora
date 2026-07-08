/**
 * Server-side environment configuration.
 *
 * This module is intentionally server-only — it reads env vars that must
 * never be bundled into the browser. Import it only from API routes, Server
 * Components, or server actions.
 */
import 'server-only';

function requireEnv(key: string, options?: { minLength?: number }): string {
  const value = process.env[key];
  if (!value) {
    throw new Error(`Missing required environment variable: ${key}`);
  }
  if (options?.minLength !== undefined && value.length < options.minLength) {
    throw new Error(`Environment variable ${key} must be at least ${options.minLength} characters`);
  }
  return value;
}

export const config = {
  /**
   * Base URL of the Go control plane API.
   * Used exclusively by BFF API routes — never sent to the browser.
   */
  orbitApiUrl: requireEnv('ORBIT_API_URL').replace(/\/$/, ''),

  /**
   * Base URL of the Go inference gateway (port 8081).
   * Used exclusively by BFF API routes — never sent to the browser.
   */
  inferenceGatewayUrl: requireEnv('INFERENCE_GATEWAY_URL').replace(/\/$/, ''),

  /**
   * Base URL of the policy gateway (port 8084).
   * Used by the BFF to fetch the principal's effective scope set on login
   * and token refresh. Never sent to the browser.
   */
  policyGatewayUrl: requireEnv('POLICY_GATEWAY_URL').replace(/\/$/, ''),

  /**
   * Base URL of the policy service PAP (port 8083).
   * Used by the BFF to proxy grant/role/vocabulary/route admin endpoints.
   * Never sent to the browser.
   */
  policyServiceUrl: requireEnv('POLICY_SERVICE_URL').replace(/\/$/, ''),

  /**
   * Base URL of the orbit-agents service.
   * Used by BFF routes for agent type and run CRUD + SSE proxy.
   * Never sent to the browser.
   */
  orbitAgentsUrl: requireEnv('ORBIT_AGENTS_URL').replace(/\/$/, ''),

  /**
   * Base URL of the MCP tool-gateway (proxy deployment, port 8096). The BFF
   * reads its read-only tool catalog (GET /v1/catalog). Never sent to the browser.
   */
  toolGatewayUrl: requireEnv('TOOL_GATEWAY_URL').replace(/\/$/, ''),

  /**
   * Secret used to sign/verify session tokens.
   * Lazy — only validated when the session layer first accesses it.
   */
  get sessionSecret() {
    return requireEnv('BFF_SESSION_SECRET', { minLength: 32 });
  },
} as const;
