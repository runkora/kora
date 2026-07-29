// ── Triggers API — client-side BFF calls ──────────────────────────────────────
//
// All functions call the BFF routes under /api/trigger-sources, /api/triggers,
// and /api/trigger-deliveries. They never throw — callers pattern-match on the
// returned `error` field.

import { ApiRoute } from '@orbit/lib/routing/api-routes';
import type {
  SchedulePreviewResponse,
  Trigger,
  TriggerCreateRequest,
  TriggerUpdateRequest,
  WebhookSourceCreateRequest,
  WebhookSourceResponse,
  WebhookSourceUpdateRequest,
} from '@orbit/shared/api';

async function errorMessage(res: Response): Promise<string> {
  const body = await res.json().catch(() => ({}) as { message?: string });
  return (body as { message?: string }).message ?? `Request failed (${res.status})`;
}

/**
 * One request, one envelope: `{ data }` on success, a message on anything else.
 *
 * Errors are values here rather than exceptions, so every caller handles the
 * network being down and the API refusing in the same shape. A 2xx with no body
 * resolves with `null` data, which is what the accepted-but-nothing-to-say
 * responses (202) actually mean.
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

/** A JSON request body, with the header that says so. */
function json(method: string, payload: unknown): RequestInit {
  return {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  };
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
 * Fires a manual trigger.
 *
 * Resolves with the delivery id only. The fire is durably queued and the run
 * starts moments later, so there is no run to name in the response; the field
 * the API once carried for it is not read, whether or not it is on the wire.
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
  // A 202 may arrive with no body: accepted, with nothing to name.
  return { deliveryId: res.data?.delivery_id ?? '', error: null };
}

// ── Deliveries ────────────────────────────────────────────────────────────────

/**
 * Replays a delivery. Resolves with the NEW delivery's id: a replay is a fresh
 * arrival that names the one it replays, so the original is left alone and the
 * caller follows the new one to see what it did.
 */
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
 * Asks the scheduler what a cron expression will actually do next.
 *
 * Errors resolve to an empty list rather than a message: the preview is a
 * supplement to the schedule builder, and a failed lookup should show nothing
 * rather than push an error at someone who is still typing. It also takes an
 * abort signal, so it stays bespoke rather than going through `send`.
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
