// RSC: loads the Marketplaces and the page of skills the URL names.

import { loadErrorMessage } from '@runkora/lib/load-error';
import { getSession, isSessionValid } from '@runkora/server/session';
import { getPreferences } from '../_preferences/preferences-server';
import { loadMarketplaces, loadSkills } from './_services/skills-server';
import { SkillsShell } from './_components/skills-shell';
import { toBrowse } from './_types';

interface SkillsPageProps {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}

export default async function SkillsIndexPage({ searchParams }: SkillsPageProps) {
  const browse = toBrowse(await searchParams);
  const empty = { marketplaces: [], browse, skills: [], total: 0, failures: [] };
  const session = await getSession();

  if (!isSessionValid(session)) {
    return (
      <SkillsShell
        initialOverview={empty}
        initialError={{ status: 401, message: loadErrorMessage(401, 'Skills') }}
      />
    );
  }

  const { skills: preferences } = await getPreferences(session.accessToken);
  const [marketplaces, page] = await Promise.all([
    loadMarketplaces(session.accessToken),
    loadSkills(session.accessToken, browse, preferences.favouriteSkills),
  ]);
  // With no Marketplaces the page shows that, not the skills request's failure.
  const listed = marketplaces.ok && marketplaces.data.length > 0;
  const failed = !marketplaces.ok ? marketplaces : listed && !page.ok ? page : null;

  return (
    <SkillsShell
      initialOverview={{
        ...empty,
        marketplaces: marketplaces.ok ? marketplaces.data : [],
        skills: page.ok ? page.data.skills : [],
        total: page.ok ? page.data.total : 0,
        failures: page.ok ? (page.data.failures ?? []) : [],
      }}
      initialError={failed && { status: failed.status, message: failed.message }}
    />
  );
}
