/**
 * /api/skills: BFF proxy for one page of the Skills overview.
 *
 * GET ?marketplace=&favourites=1&keys=a:b,c:d&search=&page=
 *     200 { data: SkillsPage }
 *     401 { code: 'unauthorized', message }
 *     4xx { code: 'upstream_error', message }  upstream refusal or not found
 *     502 { code: 'upstream_error', message }  Marketplace unavailable, with the reason
 *     500 { code: 'upstream_error', message }  any other upstream failure
 */
import 'server-only';

import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { loadSkills } from '../../(dashboard)/skills/_services/skills-server';
import { toBrowse } from '../../(dashboard)/skills/_types';

/** skills-service takes at most 200 keys. */
const MAX_KEYS = 200;

export const GET = withAuth(async (req: NextRequest, session) => {
  const query = Object.fromEntries(req.nextUrl.searchParams);
  const browse = toBrowse(query);
  // The Favourites view carries the user's keys, which the browser holds in its preferences.
  const keys = (query.keys ?? '').split(',').filter(Boolean).slice(0, MAX_KEYS);

  const result = await loadSkills(session.accessToken, browse, keys);
  if (!result.ok) {
    return NextResponse.json(
      { code: 'upstream_error', message: result.message },
      { status: result.status },
    );
  }
  return NextResponse.json({ data: result.data });
});
