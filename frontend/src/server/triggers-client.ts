import 'server-only';
import { config } from '@orbit/server/config';
import { ApiError } from './client';
import type {
  SchedulePreviewResponse,
  Trigger,
  TriggerCreateRequest,
  TriggerDelivery,
  TriggerEventTypes,
  TriggerField,
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

// GET /api/v1/trigger-sources/{id}/event-types: the provider's declared event
// types, the ones this source has sent, and the sender associations it knows.
export async function sourceEventTypes(
  accessToken: string,
  id: string,
): Promise<TriggerEventTypes> {
  const body = await request<TriggerEventTypes>(`/trigger-sources/${id}/event-types`, accessToken);
  return {
    declared: body?.declared ?? [],
    observed: body?.observed ?? [],
    associations: body?.associations ?? [],
  };
}

// GET /api/v1/trigger-providers/{provider}/event-types: the event types in the
// provider's schema, and the sender associations it knows.
export async function providerEventTypes(
  accessToken: string,
  provider: string,
): Promise<TriggerEventTypes> {
  const body = await request<TriggerEventTypes>(
    `/trigger-providers/${encodeURIComponent(provider)}/event-types`,
    accessToken,
  );
  return { declared: body?.declared ?? [], associations: body?.associations ?? [] };
}

// GET /api/v1/trigger-providers/{provider}/fields: one event type's fields,
// from the provider's schema.
export async function providerFields(
  accessToken: string,
  provider: string,
  eventType: string,
): Promise<TriggerField[]> {
  const q = new URLSearchParams({ event_type: eventType }).toString();
  const body = await request<{ fields?: TriggerField[] }>(
    `/trigger-providers/${encodeURIComponent(provider)}/fields?${q}`,
    accessToken,
  );
  return body?.fields ?? [];
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

/**
 * Response to POST /api/v1/trigger-deliveries/:id/redeliver.
 *
 * A replay is a new arrival that names the one it replays, so the caller gets a
 * new id to follow. The original is never touched.
 */
export interface RedeliverResponse {
  delivery_id: string;
  redelivery_of: string;
}

// POST /api/v1/trigger-deliveries/:id/redeliver — replay a delivery (202).
export async function redeliverDelivery(
  accessToken: string,
  id: string,
): Promise<RedeliverResponse | undefined> {
  return request<RedeliverResponse>(`/trigger-deliveries/${id}/redeliver`, accessToken, {
    method: 'POST',
  });
}

// ---------------------------------------------------------------------------
// Schedule preview
// ---------------------------------------------------------------------------

// POST /api/v1/triggers/schedule-preview — the next few fires for a cron
// expression, resolved by the same scheduler that will run it.
export async function schedulePreview(
  accessToken: string,
  payload: { cron_expression: string; timezone: string },
): Promise<SchedulePreviewResponse | undefined> {
  return request<SchedulePreviewResponse>('/triggers/schedule-preview', accessToken, {
    method: 'POST',
    body: payload,
  });
}

/**
 * One page of deliveries, newest first.
 *
 * `nextCursor` is the opaque value to echo back as `cursor` for the following
 * page; it is null on the last page. `status` and `outcome` repeat rather than
 * taking a comma list, which is what a multi-select filter sends.
 */
export interface DeliveryPage {
  deliveries: TriggerDelivery[];
  nextCursor: string | null;
}

export async function listDeliveries(
  accessToken: string,
  opts: {
    sourceId?: string;
    triggerId?: string;
    kind?: string;
    status?: string[];
    outcome?: string[];
    limit?: number;
    cursor?: string | null;
  } = {},
): Promise<DeliveryPage> {
  const qs = new URLSearchParams();
  if (opts.sourceId) qs.set('source_id', opts.sourceId);
  if (opts.triggerId) qs.set('trigger_id', opts.triggerId);
  if (opts.kind) qs.set('kind', opts.kind);
  for (const s of opts.status ?? []) qs.append('status', s);
  for (const o of opts.outcome ?? []) qs.append('outcome', o);
  if (opts.limit) qs.set('limit', String(opts.limit));
  if (opts.cursor) qs.set('cursor', opts.cursor);
  const q = qs.toString();
  const body = await request<{ deliveries?: TriggerDelivery[]; next_cursor?: string | null }>(
    `/trigger-deliveries${q ? `?${q}` : ''}`,
    accessToken,
  );
  return { deliveries: body?.deliveries ?? [], nextCursor: body?.next_cursor ?? null };
}
