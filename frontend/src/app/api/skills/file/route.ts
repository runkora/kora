/**
 * /api/skills/file: BFF proxy for one supporting file of a skill.
 *
 * GET ?marketplace=&path=&file=
 *     200 { data: SkillFile }
 *     401 { code: 'unauthorized', message }
 *     4xx { code: 'upstream_error', message }  refusal, or a file that is not listed
 *     502 { code: 'upstream_error', message }  Marketplace unavailable, with the reason
 *     500 { code: 'upstream_error', message }  any other upstream failure
 */
import 'server-only';

import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@runkora/server/route-helpers';
import { loadSkillFile } from '../../../(dashboard)/skills/_services/skills-server';

export const GET = withAuth(async (req: NextRequest, session) => {
  const query = req.nextUrl.searchParams;
  const result = await loadSkillFile(
    session.accessToken,
    query.get('marketplace') ?? '',
    query.get('path') ?? '',
    query.get('file') ?? '',
  );
  if (!result.ok) {
    return NextResponse.json(
      { code: 'upstream_error', message: result.message },
      { status: result.status },
    );
  }
  return NextResponse.json({ data: result.data });
});
