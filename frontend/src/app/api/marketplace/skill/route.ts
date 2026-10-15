/**
 * /api/marketplace/skill — BFF proxy for the details of one Skill.
 *
 * GET — returns one Skill for the authenticated session. Query:
 *       `marketplace` (the marketplaceSlug) and `skill` (the skillSlug), both
 *       from the skills-service. The Marketplace page opens the Skill in
 *       place, so the slugs never appear in the address bar.
 *
 *       Response: 200 { data: Skill }
 *                 400 { code: 'invalid_request', message: string } — a slug is missing
 *                 401 { code: 'unauthorized', message: string }
 *                 404 { code: 'not_found', message: string }       — no Skill has this slug
 *                 4xx { code: 'upstream_error', message: string }  — upstream status, such as 403
 *                 502 { code: 'upstream_error', message: string }  — any other upstream failure
 */
import 'server-only';

import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { loadSkillDetail } from '../../../(dashboard)/marketplace/_services/marketplace-server';

export const GET = withAuth(async (req: NextRequest, session) => {
  const marketplace = req.nextUrl.searchParams.get('marketplace');
  const skill = req.nextUrl.searchParams.get('skill');
  if (!marketplace || !skill) {
    return NextResponse.json(
      { code: 'invalid_request', message: 'A Marketplace slug and a Skill slug are required' },
      { status: 400 },
    );
  }

  const result = await loadSkillDetail(session.accessToken, marketplace, skill);

  if (!result.ok && result.status === 404) {
    // No Skill has this slug.
    return NextResponse.json({ code: 'not_found', message: 'Skill not found' }, { status: 404 });
  }

  if (!result.ok) {
    console.error(
      `[GET /api/marketplace/skill] upstream error ${result.status}: ${result.message}`,
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
      { code: 'upstream_error', message: 'Failed to load the Skill' },
      { status: clientStatus },
    );
  }

  return NextResponse.json({ data: result.skill });
});
