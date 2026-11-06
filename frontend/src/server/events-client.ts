import 'server-only';

import type { EventPage } from '@runkora/shared/api';
import { config } from '@runkora/server/config';
import { ApiError } from './api-error';

const UPSTREAM_TIMEOUT_MS = 10_000;

/** kora-events takes the tenant from the forwarded session token. */
async function request<T>(path: string, accessToken: string): Promise<T> {
  const response = await fetch(`${config.koraEventsUrl}${path}`, {
    headers: { Accept: 'application/json', Authorization: `Bearer ${accessToken}` },
    cache: 'no-store',
    signal: AbortSignal.timeout(UPSTREAM_TIMEOUT_MS),
  });
  if (!response.ok) {
    const err = (await response.json().catch(() => ({}))) as { code?: string; message?: string };
    throw new ApiError(response.status, err.code ?? 'UNKNOWN', err.message ?? response.statusText);
  }
  return (await response.json()) as T;
}

/** GET /v1/events: one page of the tenant's events, newest first. */
export function fetchEventPage(accessToken: string, params: URLSearchParams): Promise<EventPage> {
  return request<EventPage>(`/events?${params.toString()}`, accessToken);
}

/** Opens GET /v1/events/stream. The caller passes the body through as SSE. */
export function openEventStream(
  accessToken: string,
  params: URLSearchParams,
  lastEventId: string | null,
): Promise<Response> {
  return fetch(`${config.koraEventsUrl}/events/stream?${params.toString()}`, {
    headers: {
      Accept: 'text/event-stream',
      Authorization: `Bearer ${accessToken}`,
      ...(lastEventId !== null && { 'Last-Event-ID': lastEventId }),
    },
    cache: 'no-store',
  });
}
