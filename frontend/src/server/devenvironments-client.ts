import 'server-only';
import { config } from '@orbit/server/config';
import { ApiError } from './client';
import type {
  DevEnvironment,
  DevEnvironmentCreateRequest,
  DevEnvironmentExtendRequest,
  DevEnvironmentKubeconfigResponse,
  DevEnvironmentLimits,
} from '@orbit/shared/api';

// ---------------------------------------------------------------------------
// Core fetch helper
// ---------------------------------------------------------------------------

const UPSTREAM_TIMEOUT_MS = 10_000;

// The kubeconfig broker waits on the operator to sign a CSR — server-side
// budget is KubeconfigConfig.SignTimeoutSeconds (20s default,
// backend/internal/devenvironments/config/base.yaml). This must comfortably
// exceed that, or the BFF aborts the request before the backend ever gets a
// chance to return its own timeout error. Env-derived (rather than a bare
// constant) so an operator raising the backend's own signTimeoutSeconds
// doesn't silently desync the two — see devenvironments-client.test.ts for
// the assertion that the two stay correctly ordered.
export const DEVENV_KUBECONFIG_TIMEOUT_MS =
  Number(process.env.DEVENV_KUBECONFIG_TIMEOUT_MS) || 25_000;

// The backend default this timeout must comfortably exceed — kept here
// (rather than only in a comment) so devenvironments-client.test.ts can
// assert the ordering directly instead of hardcoding 20_000 a second time.
export const DEVENV_BACKEND_SIGN_TIMEOUT_SECONDS_DEFAULT = 20;

// Last-resort fallback only, used if a list response is somehow missing its
// limits object (never the source of truth — the backend's response is).
// Mirrors the backend's own documented defaults: orbitapi.DevEnvTTLHardCap
// (168h) and devenvironments' base.yaml quota.maxActivePerOwner (2).
const DEFAULT_LIMITS: DevEnvironmentLimits = { ttlHardCapHours: 168, maxActivePerOwner: 2 };

/**
 * Dev Environments is hosted by the identity service (the main control-plane
 * API, config.orbitApiUrl). We forward the session token; results are scoped
 * to the caller's own environments by the token's subject.
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

export interface DevEnvironmentListResult {
  environments: DevEnvironment[];
  limits: DevEnvironmentLimits;
}

export async function listDevEnvironments(accessToken: string): Promise<DevEnvironmentListResult> {
  const body = await request<{ environments?: DevEnvironment[]; limits?: DevEnvironmentLimits }>(
    '/dev-environments',
    accessToken,
  );
  return { environments: body?.environments ?? [], limits: body?.limits ?? DEFAULT_LIMITS };
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
    { method: 'POST', timeoutMs: DEVENV_KUBECONFIG_TIMEOUT_MS },
  );
}

export async function destroyDevEnvironment(accessToken: string, id: string): Promise<void> {
  await request<void>(`/dev-environments/${id}`, accessToken, { method: 'DELETE' });
}
