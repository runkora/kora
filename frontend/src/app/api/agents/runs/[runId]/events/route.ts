import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { config } from '@runkora/server/config';

type Params = { runId: string };

export const GET = withAuth(
  async (req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { runId } = await (context?.params ?? Promise.resolve({ runId: '' }));

    const upstreamUrl = `${config.koraAgentsUrl}/api/v1/runs/${runId}/events`;
    const headers: HeadersInit = {
      Accept: 'text/event-stream',
      Authorization: `Bearer ${session.accessToken}`,
    };
    const lastEventId = req.headers.get('last-event-id');
    if (lastEventId) {
      headers['Last-Event-ID'] = lastEventId;
    }

    let upstream: Response;
    try {
      upstream = await fetch(upstreamUrl, { headers, cache: 'no-store' });
    } catch {
      return NextResponse.json(
        { code: 'upstream_error', message: 'Failed to connect to event stream' },
        { status: 502 },
      );
    }

    if (!upstream.ok) {
      const errJson = (await upstream.json().catch(() => ({}))) as {
        code?: string;
        message?: string;
      };
      // Return the upstream's status + envelope directly. Throwing here would
      // propagate to Next.js's default 500 handler because withAuth does not
      // catch exceptions — the client would see an opaque 500 instead of the
      // 401/404/etc. the backend actually returned.
      return NextResponse.json(
        {
          code: errJson.code ?? 'UNKNOWN',
          message: errJson.message ?? upstream.statusText,
        },
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
  },
);
