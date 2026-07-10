import 'server-only';
import { config } from '@orbit/server/config';
import { ApiError } from './client';
import type {
  DevEnvironment,
  DevEnvironmentCreateRequest,
  DevEnvironmentExtendRequest,
  DevEnvironmentKubeconfigResponse,
} from '@orbit/shared/api';

// ---------------------------------------------------------------------------
// Core fetch helper
// ---------------------------------------------------------------------------

const UPSTREAM_TIMEOUT_MS = 10_000;

// The kubeconfig broker waits on the operator to sign a CSR — server-side
// budget is KubeconfigConfig.SignTimeoutSeconds (20s default,
// backend/internal/devenvironments/config/base.yaml). This must comfortably
// exceed that, or the BFF aborts the request before the backend ever gets a
// chance to return its own timeout error.
const KUBECONFIG_TIMEOUT_MS = 25_000;

/**
 * Dev Environments is hosted by the identity service (the main control-plane
 * API, config.orbitApiUrl) — see docs/adr/2026-07-07-ephemeral-development-environments-via-vcluster.md
 * decision 5. We forward the session token; results are scoped to the
 * caller's own environments by the token's subject.
 */
async function request<T>(
  path: string,
  accessToken: string,
  options: { method?: string; body?: unknown; timeoutMs?: number } = {},
): Promise<T | undefined> {
  const { method = 'GET', body, timeoutMs = UPSTREAM_TIMEOUT_MS } = options;
  const url = `${config.orbitApiUrl}/${path.replace(/^\//, '')}`;

  const response = await fetch(url, {
    method,
    headers: {
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      Accept: 'application/json',
      Authorization: `Bearer ${accessToken}`,
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
    cache: 'no-store',
    signal: AbortSignal.timeout(timeoutMs),
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
// Dev Environments
// ---------------------------------------------------------------------------

export async function listDevEnvironments(accessToken: string): Promise<DevEnvironment[]> {
  const body = await request<{ environments?: DevEnvironment[] }>('/dev-environments', accessToken);
  return body?.environments ?? [];
}

export async function getDevEnvironment(
  accessToken: string,
  id: string,
): Promise<DevEnvironment | undefined> {
  return request<DevEnvironment>(`/dev-environments/${id}`, accessToken);
}

export async function createDevEnvironment(
  accessToken: string,
  payload: DevEnvironmentCreateRequest,
): Promise<DevEnvironment | undefined> {
  return request<DevEnvironment>('/dev-environments', accessToken, {
    method: 'POST',
    body: payload,
  });
}

export async function extendDevEnvironment(
  accessToken: string,
  id: string,
  payload: DevEnvironmentExtendRequest,
): Promise<DevEnvironment | undefined> {
  return request<DevEnvironment>(`/dev-environments/${id}/extend`, accessToken, {
    method: 'POST',
    body: payload,
  });
}

export async function wakeDevEnvironment(
  accessToken: string,
  id: string,
): Promise<DevEnvironment | undefined> {
  return request<DevEnvironment>(`/dev-environments/${id}/wake`, accessToken, { method: 'POST' });
}

export async function kubeconfigDevEnvironment(
  accessToken: string,
  id: string,
): Promise<DevEnvironmentKubeconfigResponse | undefined> {
  return request<DevEnvironmentKubeconfigResponse>(
    `/dev-environments/${id}/kubeconfig`,
    accessToken,
    { method: 'POST', timeoutMs: KUBECONFIG_TIMEOUT_MS },
  );
}

export async function destroyDevEnvironment(accessToken: string, id: string): Promise<void> {
  await request<void>(`/dev-environments/${id}`, accessToken, { method: 'DELETE' });
}
