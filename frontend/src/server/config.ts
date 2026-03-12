/**
 * Server-side environment configuration.
 *
 * This module is intentionally server-only — it reads env vars that must
 * never be bundled into the browser. Import it only from API routes, Server
 * Components, or server actions.
 */
import "server-only";

function requireEnv(key: string, options?: { minLength?: number }): string {
  const value = process.env[key];
  if (!value) {
    throw new Error(`Missing required environment variable: ${key}`);
  }
  if (options?.minLength !== undefined && value.length < options.minLength) {
    throw new Error(
      `Environment variable ${key} must be at least ${options.minLength} characters`,
    );
  }
  return value;
}

export const config = {
  /**
   * Base URL of the Go control plane API.
   * Used exclusively by BFF API routes — never sent to the browser.
   */
  orbitApiUrl: requireEnv("ORBIT_API_URL"),

  /**
   * Secret used to sign/verify session tokens.
   */
  sessionSecret: requireEnv("BFF_SESSION_SECRET", { minLength: 32 }),
} as const;
