/**
 * /api/marketplace/skills — BFF proxy for the Marketplace overview.
 *
 * GET — returns one result page of Skills and the Marketplace list for the
 *       authenticated session. Query: `marketplace`, `query`, `page`,
 *       `pageSize`, `sort`. The route validates each value and uses the
 *       default for an invalid value.
 *
 *       Response: 200 { data: MarketplaceOverviewData }
 *                 401 { code: 'unauthorized', message: string }
 *                 499 { code: 'cancelled', message: string }       — the browser cancelled it
 *                 4xx { code: 'upstream_error', message: string }  — upstream status, such as 403
 *                 502 { code: 'upstream_error', message: string }  — any other upstream failure
 */
import 'server-only';

import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { loadMarketplaceOverview } from '../../../(dashboard)/marketplace/_services/marketplace-server';
import { parseBrowseParams } from '../../../(dashboard)/marketplace/_utils/browse-params';

export const GET = withAuth(async (req: NextRequest, session) => {
  const browse = parseBrowseParams(req.nextUrl.searchParams);
  // req.signal aborts when the browser cancels the request, so the
  // cancellation reaches the Marketplace client.
  const result = await loadMarketplaceOverview(session.accessToken, browse, req.signal);

  if (!result.ok && req.signal.aborted) {
    // The browser cancelled the request. Nobody reads this response.
    return NextResponse.json(
      { code: 'cancelled', message: 'Request cancelled' },
      { status: result.status },
    );
  }

  if (!result.ok) {
    console.error(
      `[GET /api/marketplace/skills] upstream error ${result.status}: ${result.message}`,
    );
    let clientStatus: number;
    if (result.status >= 500) {
      // An upstream failure. The browser sees a gateway error.
      clientStatus = 502;
    } else {
      // An upstream refusal, such as 403. Pass it through.
      clientStatus = result.status;
    }
    return NextResponse.json(
      { code: 'upstream_error', message: 'Failed to load Skills' },
      { status: clientStatus },
    );
  }

  return NextResponse.json({ data: result.overview });
});
