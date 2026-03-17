/**
 * Orbit API client — server-side only.
 *
 * Wraps fetch with base URL resolution, default headers, and a unified
 * error type. Import this only from Server Components or server actions.
 */
import 'server-only';

import { config } from '@/server/config';

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
    this.name = 'ApiError';
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

/** Upstream request timeout (ms). Prevents a stalled Go backend from holding
 *  the BFF worker indefinitely. AbortSignal.timeout() requires Node 18+. */
const UPSTREAM_TIMEOUT_MS = 10_000;

function buildUrl(path: string): string {
  const base = config.orbitApiUrl.replace(/\/$/, '');
  const normalizedPath = path.replace(/^\//, '');
  return `${base}/${normalizedPath}`;
}

async function request<T>(
  path: string,
  options: {
    method?: string;
    body?: unknown;
    headers?: Record<string, string>;
  } = {},
): Promise<T | undefined> {
  const { method = 'GET', body, headers: extraHeaders = {} } = options;

  const response = await fetch(buildUrl(path), {
    method,
    headers: {
      // Only set Content-Type when there is a body — GET/DELETE have none.
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      Accept: 'application/json',
      ...extraHeaders,
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
    // Disable Next.js data cache — control caching explicitly at the UI layer.
    cache: 'no-store',
    signal: AbortSignal.timeout(UPSTREAM_TIMEOUT_MS),
  });

  // The Go backend returns raw JSON — no envelope wrapper.
  // On error the body is { code, message, details? } at the top level.
  if (!response.ok) {
    // Swallow JSON parse failures on error responses — fall back to statusText.
    const errJson = await response.json().catch(() => ({}));
    const err = errJson as BackendError;
    throw new ApiError(response.status, err.code ?? 'UNKNOWN', err.message ?? response.statusText);
  }

  // 204 No Content (and any response with an explicit empty body) has no JSON
  // to parse. DELETE endpoints commonly return 204 — calling .json() on an
  // empty body throws a SyntaxError even though the request succeeded.
  if (response.status === 204 || response.headers.get('Content-Length') === '0') {
    return undefined;
  }

  // For success responses let JSON parse errors surface — a non-JSON 2xx body
  // is unexpected and should not be silently coerced to an empty object.
  return (await response.json()) as T;
}

// ---------------------------------------------------------------------------
// Public API surface
// ---------------------------------------------------------------------------

export const orbitClient = {
  get: <T>(path: string, headers?: Record<string, string>) => request<T>(path, { headers }),
  post: <T>(path: string, body: unknown, headers?: Record<string, string>) =>
    request<T>(path, { method: 'POST', body, headers }),
  patch: <T>(path: string, body: unknown, headers?: Record<string, string>) =>
    request<T>(path, { method: 'PATCH', body, headers }),
  delete: <T>(path: string, headers?: Record<string, string>) =>
    request<T>(path, { method: 'DELETE', headers }),
};
