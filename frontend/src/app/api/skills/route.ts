/**
 * /api/skills: BFF proxy for one page of the Skills overview.
 *
 * GET ?marketplace=&favourites=1&search=&page=
 *     200 { data: SkillsPage }
 *     401 { code: 'unauthorized', message }
 *     4xx { code: 'upstream_error', message }  upstream refusal or not found
 *     502 { code: 'upstream_error', message }  Marketplace unavailable, with the reason
 *     500 { code: 'upstream_error', message }  any other upstream failure
 */
import 'server-only';

import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { getPreferences } from '../../(dashboard)/_preferences/preferences-server';
import { loadSkills } from '../../(dashboard)/skills/_services/skills-server';
import { toBrowse } from '../../(dashboard)/skills/_types';

export const GET = withAuth(async (req: NextRequest, session) => {
  const browse = toBrowse(Object.fromEntries(req.nextUrl.searchParams));
  // The Favourites view reads the user's keys from the stored preferences, not the request.
  const favourites = browse.favourites
    ? (await getPreferences(session.accessToken)).skills.favouriteSkills
    : [];

  const result = await loadSkills(session.accessToken, browse, favourites);
  if (!result.ok) {
    return NextResponse.json(
      { code: 'upstream_error', message: result.message },
      { status: result.status },
    );
  }
  return NextResponse.json({ data: result.data });
});
