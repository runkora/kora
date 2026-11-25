/**
 * /api/agents/runs/stream: SSE proxy for the live run list.
 *
 * GET ?agent_type_id=&state=&created_by=&trigger_kind=&q=&after=<seq>
 * `after` stands in for Last-Event-ID on a reopened stream, which EventSource cannot set.
 * A reconnect's own Last-Event-ID header wins.
 */
import 'server-only';

import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { openRunsStream } from '@runkora/server/agents-client';
import { pickRunsStreamParams } from '@runkora/server/services/runs-server';

const UPSTREAM_CONNECT_MS = 10_000;

export const GET = withAuth(async (req: NextRequest, session) => {
  const params = req.nextUrl.searchParams;
  const lastEventId = req.headers.get('last-event-id') ?? params.get('after');

  // The browser going away ends the upstream request. An upstream that sends no headers in
  // time is dropped, while an open stream has no time limit.
  const controller = new AbortController();
  req.signal.addEventListener('abort', () => controller.abort());
  const connect = setTimeout(() => controller.abort(), UPSTREAM_CONNECT_MS);

  let upstream: Response;
  try {
    upstream = await openRunsStream(
      session.accessToken,
      pickRunsStreamParams(params),
      lastEventId,
      controller.signal,
    );
  } catch {
    return NextResponse.json(
      { code: 'upstream_error', message: 'Failed to connect to the run stream' },
      { status: 502 },
    );
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
});
