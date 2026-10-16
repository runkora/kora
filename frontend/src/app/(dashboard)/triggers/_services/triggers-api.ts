/**
 * @tier 3 — triggers-owned; also imported by the agent detail page
 * (`agents/[slug]/_components/agent-triggers-tab.tsx`). Stays Tier 3: every call
 * here is a Triggers endpoint, and the file moves with the API it wraps.
 *
 * Client-side BFF calls under /api/trigger-sources, /api/triggers and
 * /api/trigger-deliveries. They never throw — callers pattern-match on `error`.
 */

import { ApiRoute } from '@orbit/lib/routing/api-routes';
import type {
  SchedulePreviewResponse,
  Trigger,
  TriggerCreateRequest,
  TriggerDelivery,
  TriggerEventTypes,
  TriggerField,
  TriggerUpdateRequest,
  WebhookSource,
  WebhookSourceCreateRequest,
  WebhookSourceResponse,
  WebhookSourceUpdateRequest,
} from '@orbit/shared/api';

async function errorMessage(res: Response): Promise<string> {
  // `?? null` then optional chaining, not `?? ({})`: a body that is the JSON
  // literal `null` parses fine, so the catch never fires and `.message` on it
  // throws — and these functions must not throw.
  const body = (await res.json().catch(() => null)) as { message?: string } | null;
  return body?.message ?? `Request failed (${res.status})`;
}

/**
 * One request, one envelope: `{ data }` on success, a message on anything else.
 * A 2xx with no body resolves with `null` data, which is what a 202 means.
 */
async function send<T>(
  url: string,
  init?: RequestInit,
): Promise<{ data: T | null; error: null } | { data: null; error: string }> {
  let res: Response;
  try {
    res = await fetch(url, init);
  } catch {
    return { data: null, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) return { data: null, error: await errorMessage(res) };
  const body = (await res.json().catch(() => null)) as { data?: T } | null;
  return { data: body?.data ?? null, error: null };
}

/**
 * `send` for a paginated list, plus the page's cursor: `next_cursor` describes
 * the request, not any row in it, so it cannot ride inside `data`.
 */
async function sendPage<T>(
  url: string,
  init?: RequestInit,
): Promise<
  | { data: T | null; nextCursor: string | null; error: null }
  | { data: null; nextCursor: null; error: string }
> {
  let res: Response;
  try {
    res = await fetch(url, init);
  } catch {
    return { data: null, nextCursor: null, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) return { data: null, nextCursor: null, error: await errorMessage(res) };

  // An unreadable 200 is an error, not an empty page: empty is indistinguishable
  // from the end of history, so Load more would disappear and silently truncate
  // the list. The read is guarded because res.text() rejects on a body that fails
  // after the headers arrive, and this function must not throw.
  const text = await res.text().catch(() => null);
  const unreadable = {
    data: null,
    nextCursor: null,
    error: 'The server returned a response that could not be read.',
  } as const;
  if (text === null) return unreadable;
  let body: { data?: T; next_cursor?: string | null } | null;
  try {
    body = JSON.parse(text) as { data?: T; next_cursor?: string | null };
  } catch {
    return unreadable;
  }
  // A parseable body is not automatically a valid page: the route always writes
  // both keys (NextResponse.json({ data, next_cursor })), so `{}` or `null` is a
  // gateway artefact, not an empty result.
  if (body === null || typeof body !== 'object' || !Array.isArray(body.data)) {
    return unreadable;
  }
  // next_cursor must be PRESENT, not absent-and-coerced: the route always writes
  // it, so `?? null` on a body without it would read as "last page". '' is
  // refused with the absent case too — it is not null, so the grid offers Load
  // more, but the request builder only serializes a truthy cursor, so the click
  // re-fetches page one and appends it: an unbounded duplicate loop.
  const cursor = (body as { next_cursor?: unknown }).next_cursor;
  if (cursor !== null && (typeof cursor !== 'string' || cursor === '')) {
    return unreadable;
  }
  return { data: body.data as T, nextCursor: cursor, error: null };
}

function json(method: string, payload: unknown): RequestInit {
  return {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  };
}

// ── Lists ─────────────────────────────────────────────────────────────────────
//
// A missing list resolves to `[]` alongside the error, never `null`: callers
// render the empty frame and the message, not a crash.

export async function listSources(): Promise<{ sources: WebhookSource[]; error: string | null }> {
  const res = await send<WebhookSource[]>(ApiRoute.triggerSources.list(), { cache: 'no-store' });
  return { sources: res.data ?? [], error: res.error };
}

export async function listTriggers(
  agentTypeId?: string,
): Promise<{ triggers: Trigger[]; error: string | null }> {
  const res = await send<Trigger[]>(ApiRoute.triggers.list(agentTypeId), { cache: 'no-store' });
  return { triggers: res.data ?? [], error: res.error };
}

/**
 * One page of deliveries. Pass the previous call's `nextCursor` as `cursor`;
 * omit it for the first page. `nextCursor` is null on the last page.
 */
export async function listDeliveries(
  opts: {
    status?: string[];
    outcome?: string[];
    limit?: number;
    cursor?: string | null;
    omitPayload?: boolean;
  } = {},
): Promise<{
  deliveries: TriggerDelivery[];
  nextCursor: string | null;
  error: string | null;
}> {
  const qs = new URLSearchParams();
  for (const s of opts.status ?? []) qs.append('status', s);
  for (const o of opts.outcome ?? []) qs.append('outcome', o);
  if (opts.limit) qs.set('limit', String(opts.limit));
  if (opts.cursor) qs.set('cursor', opts.cursor);
  if (opts.omitPayload) qs.set('omit_payload', 'true');
  const q = qs.toString();
  const res = await sendPage<TriggerDelivery[]>(
    `${ApiRoute.triggerDeliveries.list()}${q ? `?${q}` : ''}`,
    { cache: 'no-store' },
  );
  return { deliveries: res.data ?? [], nextCursor: res.nextCursor, error: res.error };
}

// ── Sources ─────────────────────────────────────────────────────────────────

export async function createSource(
  payload: WebhookSourceCreateRequest,
): Promise<{ source: WebhookSourceResponse | null; error: string | null }> {
  const res = await send<WebhookSourceResponse>(
    ApiRoute.triggerSources.create(),
    json('POST', payload),
  );
  return { source: res.data, error: res.error };
}

export async function updateSource(
  id: string,
  payload: WebhookSourceUpdateRequest,
): Promise<{ source: WebhookSourceResponse | null; error: string | null }> {
  const res = await send<WebhookSourceResponse>(
    ApiRoute.triggerSources.detail(id),
    json('PATCH', payload),
  );
  return { source: res.data, error: res.error };
}

/** The event types this source has sent, beside its provider's declared ones. */
export async function fetchEventTypes(
  sourceId: string,
): Promise<{ eventTypes: TriggerEventTypes | null; error: string | null }> {
  const res = await send<TriggerEventTypes>(ApiRoute.triggerSources.eventTypes(sourceId), {
    cache: 'no-store',
  });
  return { eventTypes: res.data, error: res.error };
}

/** The event types in a provider's schema, and the sender associations it knows. */
export async function fetchProviderEventTypes(
  provider: string,
): Promise<{ eventTypes: TriggerEventTypes | null; error: string | null }> {
  const res = await send<TriggerEventTypes>(ApiRoute.triggerProviders.eventTypes(provider), {
    cache: 'no-store',
  });
  return { eventTypes: res.data, error: res.error };
}

/** One event type's fields, from the provider's schema. */
export async function fetchProviderFields(
  provider: string,
  eventType: string,
): Promise<{ fields: TriggerField[]; error: string | null }> {
  const res = await send<{ fields?: TriggerField[] }>(
    ApiRoute.triggerProviders.fields(provider, eventType),
    { cache: 'no-store' },
  );
  return { fields: res.data?.fields ?? [], error: res.error };
}

// ── Triggers ──────────────────────────────────────────────────────────────────

export async function createTrigger(
  payload: TriggerCreateRequest,
): Promise<{ trigger: Trigger | null; error: string | null }> {
  const res = await send<Trigger>(ApiRoute.triggers.create(), json('POST', payload));
  return { trigger: res.data, error: res.error };
}

export async function updateTrigger(
  id: string,
  payload: TriggerUpdateRequest,
): Promise<{ trigger: Trigger | null; error: string | null }> {
  const res = await send<Trigger>(ApiRoute.triggers.detail(id), json('PATCH', payload));
  return { trigger: res.data, error: res.error };
}

export async function deleteTrigger(id: string): Promise<{ error: string | null }> {
  const res = await send<never>(ApiRoute.triggers.delete(id), { method: 'DELETE' });
  return { error: res.error };
}

// ── Fire ──────────────────────────────────────────────────────────────────────

/**
 * Fires a manual trigger, resolving with the delivery id only: the fire is
 * durably queued and the run starts moments later, so there is no run to name.
 */
export async function fireTrigger(
  id: string,
  variables?: Record<string, string>,
): Promise<{ deliveryId: string; error: null } | { deliveryId: null; error: string }> {
  const res = await send<{ delivery_id?: string }>(
    ApiRoute.triggers.fire(id),
    json('POST', { variables }),
  );
  if (res.error !== null) return { deliveryId: null, error: res.error };
  return { deliveryId: res.data?.delivery_id ?? '', error: null };
}

// ── Deliveries ────────────────────────────────────────────────────────────────

/** Replays a delivery, resolving with the NEW delivery's id; the original is untouched. */
export async function redeliverDelivery(
  id: string,
): Promise<{ deliveryId: string; error: null } | { deliveryId: null; error: string }> {
  const res = await send<{ delivery_id?: string }>(ApiRoute.triggerDeliveries.redeliver(id), {
    method: 'POST',
  });
  if (res.error !== null) return { deliveryId: null, error: res.error };
  return { deliveryId: res.data?.delivery_id ?? '', error: null };
}

// ── Schedule preview ──────────────────────────────────────────────────────────

/**
 * Asks the scheduler what a cron expression will actually do next. Bespoke
 * rather than through `send`: it takes an abort signal, and a failed lookup
 * resolves null so nothing is pushed at someone still typing.
 */
export async function fetchSchedulePreview(
  cronExpression: string,
  timezone: string,
  signal?: AbortSignal,
): Promise<SchedulePreviewResponse | null> {
  try {
    const res = await fetch(ApiRoute.triggers.schedulePreview(), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ cron_expression: cronExpression, timezone }),
      signal,
    });
    if (!res.ok) return null;
    const body = (await res.json()) as { data?: SchedulePreviewResponse };
    return body.data ?? null;
  } catch {
    return null;
  }
}
