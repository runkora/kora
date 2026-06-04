/**
 * orbit-agents HTTP client — server-side only.
 *
 * Wraps fetch with base URL resolution, bearer token forwarding, and a
 * unified error type matching the Go backend's error envelope.
 * Import only from API route handlers or Server Components.
 */
import 'server-only';

import { config } from './config';
import { ApiError } from './client';

// ---------------------------------------------------------------------------
// Core fetch helper
// ---------------------------------------------------------------------------

const UPSTREAM_TIMEOUT_MS = 10_000;

async function request<T>(
  path: string,
  accessToken: string,
  options: { method?: string; body?: unknown } = {},
): Promise<T | undefined> {
  const { method = 'GET', body } = options;
  const url = `${config.orbitAgentsUrl}/${path.replace(/^\//, '')}`;

  const response = await fetch(url, {
    method,
    headers: {
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      Accept: 'application/json',
      Authorization: `Bearer ${accessToken}`,
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
    cache: 'no-store',
    signal: AbortSignal.timeout(UPSTREAM_TIMEOUT_MS),
  });

  if (!response.ok) {
    const errJson = await response.json().catch(() => ({}));
    const err = errJson as { code?: string; message?: string };
    throw new ApiError(response.status, err.code ?? 'UNKNOWN', err.message ?? response.statusText);
  }

  if (response.status === 204 || response.headers.get('Content-Length') === '0') {
    return undefined;
  }

  return (await response.json()) as T;
}

// ---------------------------------------------------------------------------
// Public API surface
// ---------------------------------------------------------------------------

export function agentsClient(accessToken: string) {
  return {
    get: <T>(path: string) => request<T>(path, accessToken),
    post: <T>(path: string, body: unknown) =>
      request<T>(path, accessToken, { method: 'POST', body }),
    patch: <T>(path: string, body: unknown) =>
      request<T>(path, accessToken, { method: 'PATCH', body }),
    delete: <T>(path: string) => request<T>(path, accessToken, { method: 'DELETE' }),
  };
}
