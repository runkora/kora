/**
 * @tier 3 — triggers-owned, with two feature-area consumers: the Triggers page
 * and the agent detail page (`agents/[slug]/_components/agent-triggers-tab.tsx`),
 * which imports it across feature areas. Stays Tier 3 deliberately: every call
 * here is a Triggers endpoint, and the file moves with the API it wraps.
 *
 * Triggers API — client-side BFF calls.
 *
 * All functions call the BFF routes under /api/trigger-sources, /api/triggers,
 * and /api/trigger-deliveries. They never throw — callers pattern-match on the
 * returned `error` field.
 */

import { writeErrorMessage } from '@runkora/lib/declarative';
import { ApiRoute } from '@runkora/lib/routing/api-routes';
import type {
  ModelObject,
  SchedulePreviewResponse,
  Trigger,
  TriggerCreateRequest,
  TriggerDelivery,
  TriggerEventTypes,
  TriggerField,
  TriggerRoutingCandidate,
  TriggerUpdateRequest,
  WebhookSource,
  WebhookSourceCreateRequest,
  WebhookSourceResponse,
  WebhookSourceUpdateRequest,
} from '@runkora/shared/api';

interface ErrorBody {
  message?: string;
  details?: { missing_scopes?: unknown };
}

/**
 * The message to show for a refused request. A routed trigger the caller cannot
 * grant is refused with the scopes it lacks, and those are what the person
 * needs to ask for, so they replace the upstream's sentence.
 */
export function errorMessageOf(status: number, body: ErrorBody): string {
  const missing = body.details?.missing_scopes;
  if (Array.isArray(missing) && missing.length > 0) {
    return `You do not hold every scope this trigger needs. Missing: ${missing.join(', ')}.`;
  }
  return writeErrorMessage(status, body);
}

async function errorMessage(res: Response): Promise<string> {
  const body = (await res.json().catch(() => ({}))) as ErrorBody;
  return errorMessageOf(res.status, body);
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

// ── Lists ─────────────────────────────────────────────────────────────────────
//
// Read the same envelopes the platform Triggers page is served, so any surface
// that lists triggers goes through one client and one error shape. A missing
// list resolves to `[]` alongside the error, never `null`: callers render the
// empty frame and the message, not a crash.

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

export async function listDeliveries(): Promise<{
  deliveries: TriggerDelivery[];
  error: string | null;
}> {
  const res = await send<TriggerDelivery[]>(ApiRoute.triggerDeliveries.list(), {
    cache: 'no-store',
  });
  return { deliveries: res.data ?? [], error: res.error };
}

/** The agent types a routed trigger may name, each marked runnable for the caller. */
export async function listRoutingCandidates(): Promise<{
  candidates: TriggerRoutingCandidate[];
  error: string | null;
}> {
  const res = await send<TriggerRoutingCandidate[]>(ApiRoute.triggers.routingCandidates(), {
    cache: 'no-store',
  });
  return { candidates: res.data ?? [], error: res.error };
}

/** The tenant's model catalog, as the caller may use it. */
export async function listModels(): Promise<{ models: ModelObject[]; error: string | null }> {
  const res = await send<ModelObject[]>(ApiRoute.models.GET(), { cache: 'no-store' });
  return { models: res.data ?? [], error: res.error };
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

/** The event types in a provider's schema, its category and the sender roles it reports. */
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
