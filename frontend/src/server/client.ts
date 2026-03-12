/**
 * Orbit API client — server-side only.
 *
 * Wraps fetch with base URL resolution, default headers, and a unified
 * error type. Import this only from Server Components or server actions.
 */
import "server-only";

import { config } from "@/server/config";

// ---------------------------------------------------------------------------
// Error types
// ---------------------------------------------------------------------------

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }

  get isNotFound() {
    return this.status === 404;
  }

  get isConflict() {
    return this.status === 409;
  }

  get isUnauthorized() {
    return this.status === 401;
  }
}

// ---------------------------------------------------------------------------
// Backend error shape (Go backend returns errors at the top level, not nested)
// { code: string; message: string; details?: Record<string, unknown> }
// ---------------------------------------------------------------------------

interface BackendError {
  code: string;
  message: string;
  details?: Record<string, unknown>;
}

// ---------------------------------------------------------------------------
// Core fetch helper
// ---------------------------------------------------------------------------

function buildUrl(path: string): string {
  const base = config.orbitApiUrl.replace(/\/$/, "");
  const normalizedPath = path.replace(/^\//, "");
  return `${base}/${normalizedPath}`;
}

async function request<T>(
  path: string,
  options: { headers?: Record<string, string> } = {},
): Promise<T> {
  const { headers: extraHeaders = {} } = options;

  const response = await fetch(buildUrl(path), {
    method: "GET",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json",
      ...extraHeaders,
    },
    // Disable Next.js data cache — control caching explicitly at the UI layer.
    cache: "no-store",
  });

  // The Go backend returns raw JSON — no envelope wrapper.
  // On error the body is { code, message, details? } at the top level.
  const json = await response.json().catch(() => ({}));

  if (!response.ok) {
    const err = json as BackendError;
    throw new ApiError(
      response.status,
      err.code ?? "UNKNOWN",
      err.message ?? response.statusText,
    );
  }

  return json as T;
}

// ---------------------------------------------------------------------------
// Public API surface
// ---------------------------------------------------------------------------

export const orbitClient = {
  get: <T>(path: string, headers?: Record<string, string>) =>
    request<T>(path, { headers }),
};
