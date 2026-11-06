// Client-side calls to the events BFF routes. Never calls kora-events directly.

import { ApiRoute } from '@runkora/lib/routing/api-routes';
import type { EventPage } from '../_types';

export interface FetchEventPageResult {
  page: EventPage | null;
  error: string | null;
}

/** One page of events. Returns the error as a sentence, never throws. */
export async function fetchEventPage(params: URLSearchParams): Promise<FetchEventPageResult> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.events.page(params), { cache: 'no-store' });
  } catch {
    return { page: null, error: 'Could not reach the server.' };
  }
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { message?: string };
    return { page: null, error: body.message ?? `Request failed (${res.status})` };
  }
  try {
    const body = (await res.json()) as { data: EventPage };
    return { page: body.data, error: null };
  } catch {
    return { page: null, error: 'The server sent a response the page could not read.' };
  }
}

/** The platform event types from kora-triggers, or none when the caller cannot read them. */
export async function fetchEventTypes(): Promise<string[]> {
  try {
    const res = await fetch(ApiRoute.triggers.eventTypes(), { cache: 'no-store' });
    if (!res.ok) return [];
    const body = (await res.json()) as { data?: string[] };
    return body.data ?? [];
  } catch {
    return [];
  }
}

/** Opens the live stream. `after` in the params replays every event past that seq first. */
export function openEventStream(params: URLSearchParams): EventSource {
  return new EventSource(ApiRoute.events.stream(params));
}
