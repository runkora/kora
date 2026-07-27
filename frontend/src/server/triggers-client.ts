import 'server-only';
import { config } from '@orbit/server/config';
import { ApiError } from './client';
import type {
  Trigger,
  TriggerCreateRequest,
  TriggerDelivery,
  TriggerFireRequest,
  TriggerFireResponse,
  TriggerUpdateRequest,
  WebhookSource,
  WebhookSourceCreateRequest,
  WebhookSourceResponse,
  WebhookSourceUpdateRequest,
} from '@orbit/shared/api';

// ---------------------------------------------------------------------------
// Core fetch helper
// ---------------------------------------------------------------------------

const UPSTREAM_TIMEOUT_MS = 10_000;

/**
 * The Triggers feature is served by orbit-triggers, the platform's front door for
 * starting work. It is separate from identity so that unauthenticated,
 * internet-reachable webhook ingestion does not share a process with the token
 * authority; identity still mints each trigger's SERVICE token over the mesh.
 * We forward the session token; results are tenant-scoped from the token.
 * See docs/adr/2026-06-20-extended-trigger-types.md.
 */
async function request<T>(
  path: string,
  accessToken: string,
  options: { method?: string; body?: unknown } = {},
): Promise<T | undefined> {
  const { method = 'GET', body } = options;
  const url = `${config.orbitTriggersUrl}/${path.replace(/^\//, '')}`;

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
// Sources
// ---------------------------------------------------------------------------

export async function listSources(accessToken: string): Promise<WebhookSource[]> {
  const body = await request<{ sources?: WebhookSource[] }>('/trigger-sources', accessToken);
  return body?.sources ?? [];
}

export async function createSource(
  accessToken: string,
  payload: WebhookSourceCreateRequest,
): Promise<WebhookSourceResponse | undefined> {
  return request<WebhookSourceResponse>('/trigger-sources', accessToken, {
    method: 'POST',
    body: payload,
  });
}

export async function updateSource(
  accessToken: string,
  id: string,
  payload: WebhookSourceUpdateRequest,
): Promise<WebhookSourceResponse | undefined> {
  return request<WebhookSourceResponse>(`/trigger-sources/${id}`, accessToken, {
    method: 'PATCH',
    body: payload,
  });
}

export async function deleteSource(accessToken: string, id: string): Promise<void> {
  await request<void>(`/trigger-sources/${id}`, accessToken, { method: 'DELETE' });
}

// ---------------------------------------------------------------------------
// Triggers
// ---------------------------------------------------------------------------

export async function listTriggers(accessToken: string, agentTypeId?: string): Promise<Trigger[]> {
  const q = agentTypeId ? `?agent_type_id=${encodeURIComponent(agentTypeId)}` : '';
  const body = await request<{ triggers?: Trigger[] }>(`/triggers${q}`, accessToken);
  return body?.triggers ?? [];
}

export async function createTrigger(
  accessToken: string,
  payload: TriggerCreateRequest,
): Promise<Trigger | undefined> {
  return request<Trigger>('/triggers', accessToken, { method: 'POST', body: payload });
}

export async function updateTrigger(
  accessToken: string,
  id: string,
  payload: TriggerUpdateRequest,
): Promise<Trigger | undefined> {
  return request<Trigger>(`/triggers/${id}`, accessToken, {
    method: 'PATCH',
    body: payload,
  });
}

export async function deleteTrigger(accessToken: string, id: string): Promise<void> {
  await request<void>(`/triggers/${id}`, accessToken, { method: 'DELETE' });
}

// POST /api/v1/triggers/:id/fire — fire a manual trigger. The run executes
// under the trigger's own SERVICE principal, not the caller's identity.
export async function fireTrigger(
  accessToken: string,
  id: string,
  payload: TriggerFireRequest,
): Promise<TriggerFireResponse | undefined> {
  return request<TriggerFireResponse>(`/triggers/${id}/fire`, accessToken, {
    method: 'POST',
    body: payload,
  });
}

// ---------------------------------------------------------------------------
// Deliveries
// ---------------------------------------------------------------------------

export async function listDeliveries(
  accessToken: string,
  opts: { sourceId?: string; limit?: number } = {},
): Promise<TriggerDelivery[]> {
  const qs = new URLSearchParams();
  if (opts.sourceId) qs.set('source_id', opts.sourceId);
  if (opts.limit) qs.set('limit', String(opts.limit));
  const q = qs.toString();
  const body = await request<{ deliveries?: TriggerDelivery[] }>(
    `/trigger-deliveries${q ? `?${q}` : ''}`,
    accessToken,
  );
  return body?.deliveries ?? [];
}
