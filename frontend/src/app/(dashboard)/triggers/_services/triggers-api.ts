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

// ── Sources ─────────────────────────────────────────────────────────────────

export async function createSource(
  payload: WebhookSourceCreateRequest,
): Promise<{ source: WebhookSourceResponse | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.triggerSources.create(), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
  } catch {
    return { source: null, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) return { source: null, error: await errorMessage(res) };
  const body = (await res.json()) as { data: WebhookSourceResponse };
  return { source: body.data, error: null };
}

export async function updateSource(
  id: string,
  payload: WebhookSourceUpdateRequest,
): Promise<{ source: WebhookSourceResponse | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.triggerSources.detail(id), {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
  } catch {
    return { source: null, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) return { source: null, error: await errorMessage(res) };
  const body = (await res.json()) as { data: WebhookSourceResponse };
  return { source: body.data, error: null };
}

// ── Triggers ──────────────────────────────────────────────────────────────────

export async function createTrigger(
  payload: TriggerCreateRequest,
): Promise<{ trigger: Trigger | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.triggers.create(), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
  } catch {
    return { trigger: null, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) return { trigger: null, error: await errorMessage(res) };
  const body = (await res.json()) as { data: Trigger };
  return { trigger: body.data, error: null };
}

export async function updateTrigger(
  id: string,
  payload: TriggerUpdateRequest,
): Promise<{ trigger: Trigger | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.triggers.detail(id), {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
  } catch {
    return { trigger: null, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) return { trigger: null, error: await errorMessage(res) };
  const body = (await res.json()) as { data: Trigger };
  return { trigger: body.data, error: null };
}

export async function deleteTrigger(id: string): Promise<{ error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.triggers.delete(id), { method: 'DELETE' });
  } catch {
    return { error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) return { error: await errorMessage(res) };
  return { error: null };
}

// ── Fire ──────────────────────────────────────────────────────────────────────

export async function fireTrigger(
  id: string,
  variables?: Record<string, string>,
): Promise<{ deliveryId: string; runId: string; error: null } | { error: string }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.triggers.fire(id), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ variables }),
    });
  } catch {
    return { error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) return { error: await errorMessage(res) };
  // A 202 may arrive with no body; treat a missing envelope as accepted with
  // nothing to name rather than throwing on an undefined read.
  const body = (await res.json().catch(() => null)) as {
    data?: { delivery_id?: string; run_id?: string };
  } | null;
  return {
    deliveryId: body?.data?.delivery_id ?? '',
    runId: body?.data?.run_id ?? '',
    error: null,
  };
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
  let res: Response;
  try {
    res = await fetch(ApiRoute.triggerDeliveries.redeliver(id), { method: 'POST' });
  } catch {
    return { deliveryId: null, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) return { deliveryId: null, error: await errorMessage(res) };
  const body = (await res.json().catch(() => null)) as {
    data?: { delivery_id?: string };
  } | null;
  return { deliveryId: body?.data?.delivery_id ?? '', error: null };
}

// ── Schedule preview ──────────────────────────────────────────────────────────

/**
 * Asks the scheduler what a cron expression will actually do next.
 *
 * Errors resolve to an empty list rather than a message: the preview is a
 * supplement to the schedule builder, and a failed lookup should show nothing
 * rather than push an error at someone who is still typing.
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
