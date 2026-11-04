// RSC: /skills opens the first Marketplace, or says why there is none.

import { redirect } from 'next/navigation';
import { APP_ROUTES } from '@runkora/lib/routing';
import { loadErrorMessage } from '@runkora/lib/load-error';
import { getSession, isSessionValid } from '@runkora/server/session';
import { loadMarketplaces } from './_services/skills-server';
import { SkillsShell } from './_components/skills-shell';

const NO_MARKETPLACE = {
  marketplaces: [],
  browse: { marketplace: '', search: '', page: 1 },
  skills: [],
  total: 0,
};

export default async function SkillsIndexPage() {
  const session = await getSession();
  const result = isSessionValid(session)
    ? await loadMarketplaces(session.accessToken)
    : { ok: false as const, status: 401, message: loadErrorMessage(401, 'Skills') };

  if (result.ok && result.data[0]) redirect(APP_ROUTES.skills.marketplace(result.data[0].slug));

  return <SkillsShell initialOverview={NO_MARKETPLACE} initialError={result.ok ? null : result} />;
}
