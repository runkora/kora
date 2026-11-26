/**
 * /api/agents/runs/stream: SSE proxy for the live run list.
 *
 * GET ?agent_type_id=&state=&created_by=&trigger_kind=&q=&after=<seq>
 */
import 'server-only';

import { withAuth } from '@runkora/server/route-helpers';
import { openRunsStream } from '@runkora/server/agents-client';
import { proxySse } from '@runkora/server/sse-proxy';
import { pickRunsStreamParams } from '@runkora/server/services/runs-server';

export const GET = withAuth((req, session) =>
  proxySse(
    req,
    (lastEventId, signal) =>
      openRunsStream(
        session.accessToken,
        pickRunsStreamParams(req.nextUrl.searchParams),
        lastEventId,
        signal,
      ),
    'Failed to connect to the run stream',
  ),
);
