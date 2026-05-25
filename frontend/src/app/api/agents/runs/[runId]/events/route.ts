import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { config } from '@orbit/server/config';
import { ApiError } from '@orbit/server/client';

type Params = { runId: string };

export const GET = withAuth(
  async (req: NextRequest, session, context?: { params: Promise<Params> }) => {
    const { runId } = await (context?.params ?? Promise.resolve({ runId: '' }));

    const upstreamUrl = `${config.orbitAgentsUrl}/api/v1/runs/${runId}/events`;
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
      throw new ApiError(
        upstream.status,
        errJson.code ?? 'UNKNOWN',
        errJson.message ?? upstream.statusText,
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
