import 'server-only';
import { config } from '@orbit/server/config';
import { ApiError } from './client';
import type {
  Environment,
  EnvironmentRequest,
  EnvironmentSizes,
  SandboxImage,
} from '@orbit/shared/api';

// ---------------------------------------------------------------------------
// Core fetch helper
// ---------------------------------------------------------------------------

const UPSTREAM_TIMEOUT_MS = 10_000;

/**
 * orbit-agents serves environments and sandbox images over the mesh. The session
 * token is forwarded and orbit-agents scopes every result to the caller's tenant.
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

/** Lists the sandbox images registered in the caller's tenant, with their validity. */
export async function listSandboxImages(accessToken: string): Promise<SandboxImage[]> {
  const body = await request<{ sandbox_images?: SandboxImage[] }>(
    '/api/v1/sandbox-images',
    accessToken,
  );
  return body?.sandbox_images ?? [];
}

/** Reads the platform's default and maximum environment sizes. */
export async function getEnvironmentSizes(accessToken: string): Promise<EnvironmentSizes | null> {
  return (await request<EnvironmentSizes>('/api/v1/environment-sizes', accessToken)) ?? null;
}
