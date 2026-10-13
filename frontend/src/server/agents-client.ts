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
import type {
  AgentInsightsPeriod,
  AgentRunStats,
  EffectiveAccess,
  EffectiveAccessPreviewRequest,
  RunsPage,
} from '@orbit/shared/api';

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

/**
 * Fetches the resolved effective-access view for an agent type (by UUID) from
 * orbit-agents. Returns the union of its own egress posture, its referenced
 * environment, and connector-derived auto-egress.
 */
export async function fetchEffectiveAccess(
  accessToken: string,
  agentTypeId: string,
): Promise<EffectiveAccess | undefined> {
  return request<EffectiveAccess>(
    `/api/v1/agent-types/${agentTypeId}/effective-access`,
    accessToken,
  );
}

/**
 * Resolves the effective-access view for a *draft* (unsaved) connectivity
 * config — backs the Connectivity tab's live preview as the user edits.
 * Nothing is persisted server-side.
 */
export async function previewEffectiveAccess(
  accessToken: string,
  body: EffectiveAccessPreviewRequest,
): Promise<EffectiveAccess | undefined> {
  return request<EffectiveAccess>('/api/v1/agent-types/effective-access', accessToken, {
    method: 'POST',
    body,
  });
}

/** One page of the runs the caller may read, newest first. orbit-agents validates the params. */
export async function listRuns(
  accessToken: string,
  params: URLSearchParams,
): Promise<RunsPage | undefined> {
  return request<RunsPage>(`/api/v1/runs?${params.toString()}`, accessToken);
}

/** Run stats across every agent type whose runs the caller may read. */
export async function fetchTenantRunStats(
  accessToken: string,
  period: AgentInsightsPeriod,
): Promise<AgentRunStats | undefined> {
  return request<AgentRunStats>(`/api/v1/runs/stats?period=${period}`, accessToken);
}
