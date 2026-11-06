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
  const body = (await res.json()) as { data: EventPage };
  return { page: body.data, error: null };
}

/** Opens the live stream. `after` in the params replays every event past that seq first. */
export function openEventStream(params: URLSearchParams): EventSource {
  return new EventSource(ApiRoute.events.stream(params));
}
