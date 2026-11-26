/**
 * Server-Sent Events pass-through for BFF routes: opens an upstream stream with the session
 * token and hands its body to the browser unchanged.
 */
import 'server-only';

import { type NextRequest, NextResponse } from 'next/server';

const UPSTREAM_CONNECT_MS = 10_000;

/** Opens an upstream SSE GET, resuming past `lastEventId` when one is given. */
export function openSse(
  url: string,
  accessToken: string,
  lastEventId: string | null,
  signal: AbortSignal,
): Promise<Response> {
  return fetch(url, {
    headers: {
      Accept: 'text/event-stream',
      Authorization: `Bearer ${accessToken}`,
      ...(lastEventId !== null && { 'Last-Event-ID': lastEventId }),
    },
    cache: 'no-store',
    signal,
  });
}

/**
 * Proxies the stream `open` returns. `after` stands in for Last-Event-ID on a stream the page
 * opens itself, which EventSource cannot give a header; a reconnect's own header wins.
 */
export async function proxySse(
  req: NextRequest,
  open: (lastEventId: string | null, signal: AbortSignal) => Promise<Response>,
  unreachable: string,
): Promise<NextResponse> {
  // An empty header or `after` carries no position, so it counts as absent.
  const lastEventId =
    req.headers.get('last-event-id') || req.nextUrl.searchParams.get('after') || null;

  // The browser going away ends the upstream request. An upstream that sends no headers in
  // time is dropped, while an open stream has no time limit.
  const controller = new AbortController();
  req.signal.addEventListener('abort', () => controller.abort());
  const connect = setTimeout(() => controller.abort(), UPSTREAM_CONNECT_MS);

  let upstream: Response;
  try {
    upstream = await open(lastEventId, controller.signal);
  } catch {
    return NextResponse.json({ code: 'upstream_error', message: unreachable }, { status: 502 });
  } finally {
    clearTimeout(connect);
  }

  if (!upstream.ok) {
    const err = (await upstream.json().catch(() => ({}))) as { code?: string; message?: string };
    return NextResponse.json(
      { code: err.code ?? 'UNKNOWN', message: err.message ?? upstream.statusText },
      { status: upstream.status },
    );
  }

  return new NextResponse(upstream.body, {
    status: 200,
    headers: {
      'Content-Type': 'text/event-stream',
      'Cache-Control': 'no-cache',
      Connection: 'keep-alive',
      'X-Accel-Buffering': 'no',
    },
  });
}
