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
 * Calls orbit-triggers, a separate process from identity so that
 * internet-reachable webhook ingestion does not share one with the token
 * authority (docs/adr/2026-06-20-extended-trigger-types.md). The session token is
 * forwarded; results are tenant-scoped from it.
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

// The run executes under the trigger's own SERVICE principal, not the caller's.
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

/** A replay is a new arrival that names the one it replays; the original is untouched. */
export interface RedeliverResponse {
  delivery_id: string;
  redelivery_of: string;
}

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

// Resolved by the same scheduler that will run the cron expression.
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
 * One page of deliveries, newest first. `nextCursor` is the opaque value to echo
 * back as `cursor`; it is null on the last page.
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
    /** Leaves the full payload out; the excerpt, outcomes and runs stay. */
    omitPayload?: boolean;
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
  if (opts.omitPayload) qs.set('omit_payload', 'true');
  const q = qs.toString();
  const env = await request<{
    deliveries?: unknown;
    has_next_page?: unknown;
    next_cursor?: unknown;
  } | null>(`/trigger-deliveries${q ? `?${q}` : ''}`, accessToken);

  // The platform API always writes all three keys, so a body missing one is a
  // malformed 200 (version skew, a proxy's error page), not a page. Coercing it
  // here would re-wrap it as a well-formed `{ data, next_cursor }` and leave the
  // browser's own guard nothing to catch: Load more gone, list silently truncated.
  //
  // has_next_page carries the check because it is the only field that is always
  // a plain boolean. `deliveries` is null on an empty page - Go marshals the nil
  // slice the repository returns for no rows - so an array check would reject a
  // tenant's first visit.
  if (env == null || typeof env.has_next_page !== 'boolean') {
    throw new ApiError(502, 'bad_upstream_response', 'deliveries response was not a page');
  }
  // '' is refused with the absent case: it is not null, so a consumer offers
  // Load more, but a falsy cursor is omitted from the next request and the
  // backend answers page one again - an unbounded duplicate loop, not a page walk.
  const cursor = env.next_cursor;
  if (cursor !== null && (typeof cursor !== 'string' || cursor === '')) {
    throw new ApiError(
      502,
      'bad_upstream_response',
      'deliveries response had no usable next_cursor',
    );
  }
  if (env.deliveries !== null && !Array.isArray(env.deliveries)) {
    throw new ApiError(502, 'bad_upstream_response', 'deliveries response had no delivery list');
  }
  // Upstream derives has_next_page as `nextCursor != ""` and writes next_cursor
  // only when non-empty, so the two cannot disagree. If they do, this is not the
  // envelope this client was written against.
  if (env.has_next_page !== (cursor !== null)) {
    throw new ApiError(
      502,
      'bad_upstream_response',
      'deliveries response disagreed with itself about a next page',
    );
  }
  return { deliveries: (env.deliveries ?? []) as TriggerDelivery[], nextCursor: cursor };
}
