/**
 * /api/events/stream: SSE proxy for the live event stream.
 *
 * GET ?category=&type=&agent=&principal=&after=<seq>
 */
import 'server-only';

import { withAuth } from '@runkora/server/route-helpers';
import { openEventStream } from '@runkora/server/events-client';
import { proxySse } from '@runkora/server/sse-proxy';
import { STREAM_PARAMS, pickParams } from '../../../(dashboard)/events/_services/events-server';

export const GET = withAuth((req, session) =>
  proxySse(
    req,
    (lastEventId, signal) =>
      openEventStream(
        session.accessToken,
        pickParams(req.nextUrl.searchParams, STREAM_PARAMS),
        lastEventId,
        signal,
      ),
    'Failed to connect to the event stream',
  ),
);
