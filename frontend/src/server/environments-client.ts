import 'server-only';
import { config } from '@orbit/server/config';
import { ApiError } from './client';
import type { Environment, EnvironmentRequest } from '@orbit/shared/api';

// ---------------------------------------------------------------------------
// Core fetch helper
// ---------------------------------------------------------------------------

const UPSTREAM_TIMEOUT_MS = 10_000;

/**
 * Environments are a tenant-scoped, UI-managed bundle of egress hosts +
 * connectors served by orbit-agents — the same normal HTTP service the frontend
 * reaches over the mesh for agent-types and the connection catalog. We forward
 * the session token; the result is scoped to the caller's tenant from the token,
 * so no tenant param is needed.
 */
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

/** Lists the caller's tenant's environments from orbit-agents. */
export async function listEnvironments(accessToken: string): Promise<Environment[]> {
  const body = await request<{ environments?: Environment[] }>('/api/v1/environments', accessToken);
  return body?.environments ?? [];
}

/** Creates an environment (201). */
export async function createEnvironment(
  accessToken: string,
  payload: EnvironmentRequest,
): Promise<Environment | undefined> {
  return request<Environment>('/api/v1/environments', accessToken, {
    method: 'POST',
    body: payload,
  });
}

/** Replaces an environment in full (PUT, 200). */
export async function updateEnvironment(
  accessToken: string,
  id: string,
  payload: EnvironmentRequest,
): Promise<Environment | undefined> {
  return request<Environment>(`/api/v1/environments/${id}`, accessToken, {
    method: 'PUT',
    body: payload,
  });
}

/** Deletes an environment (204). */
export async function deleteEnvironment(accessToken: string, id: string): Promise<void> {
  await request<void>(`/api/v1/environments/${id}`, accessToken, { method: 'DELETE' });
}
