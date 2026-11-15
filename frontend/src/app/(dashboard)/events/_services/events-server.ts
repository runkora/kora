import 'server-only';

import type { EventPage } from '@runkora/shared/api';
import { fetchEventPage } from '@runkora/server/events-client';
import { loadFailure, type Loaded } from '@runkora/server/load-failure';

/** The list filters the BFF passes on; anything else in the request is dropped. */
const LIST_PARAMS = ['category', 'type', 'agent', 'principal', 'q', 'cursor', 'limit'];

/** The stream filters. The stream takes no search. */
export const STREAM_PARAMS = ['category', 'type', 'agent', 'principal'];

/** Copies the allowed parameters, keeping every value of a repeated one. */
export function pickParams(source: URLSearchParams, allowed: string[]): URLSearchParams {
  const params = new URLSearchParams();
  for (const [key, value] of source) if (allowed.includes(key)) params.append(key, value);
  return params;
}

export async function loadEventPage(
  accessToken: string,
  query: URLSearchParams,
): Promise<Loaded<EventPage>> {
  try {
    return { ok: true, data: await fetchEventPage(accessToken, pickParams(query, LIST_PARAMS)) };
  } catch (err) {
    console.error('[loadEventPage] failed:', err);
    return loadFailure(err, 'the events');
  }
}
