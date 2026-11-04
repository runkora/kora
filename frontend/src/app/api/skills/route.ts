/**
 * /api/skills: BFF proxy for one page of a Marketplace's skills.
 *
 * GET ?marketplace=&search=&page=&pageSize=
 *     200 { data: MarketplaceSkillsPage }
 *     401 { code: 'unauthorized', message }
 *     4xx { code: 'upstream_error', message }  upstream refusal or not found
 *     502 { code: 'upstream_error', message }  Marketplace unavailable, with the reason
 *     500 { code: 'upstream_error', message }  any other upstream failure
 */
import 'server-only';

import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { PREFERENCE_DEFAULTS } from '../../(dashboard)/_preferences/preferences.schema';
import { loadSkillsPage } from '../../(dashboard)/skills/_services/skills-server';
import { toBrowse } from '../../(dashboard)/skills/_types';

export const GET = withAuth(async (req: NextRequest, session) => {
  const query = req.nextUrl.searchParams;
  const browse = toBrowse(query.get('marketplace') ?? '', query.get('search'), query.get('page'));
  const size = Number(query.get('pageSize'));
  const pageSize =
    Number.isInteger(size) && size > 0 ? size : PREFERENCE_DEFAULTS.tables.defaultPageSize;

  const result = await loadSkillsPage(session.accessToken, browse, pageSize);
  if (!result.ok) {
    return NextResponse.json(
      { code: 'upstream_error', message: result.message },
      { status: result.status },
    );
  }
  return NextResponse.json({ data: result.data });
});
