// RSC: loads the Marketplaces and the page of skills the URL names.

import { loadErrorMessage } from '@runkora/lib/load-error';
import { getSession, isSessionValid } from '@runkora/server/session';
import { getPreferences } from '../../_preferences/preferences-server';
import { loadMarketplaces, loadSkillsPage } from '../_services/skills-server';
import { SkillsShell } from '../_components/skills-shell';
import { decodeParam, toBrowse } from '../_types';

interface SkillsMarketplacePageProps {
  params: Promise<{ marketplace: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}

export default async function SkillsMarketplacePage({
  params,
  searchParams,
}: SkillsMarketplacePageProps) {
  const [{ marketplace }, query] = await Promise.all([params, searchParams]);
  const browse = toBrowse(decodeParam(marketplace), query.search, query.page);
  const session = await getSession();

  if (!isSessionValid(session)) {
    return (
      <SkillsShell
        initialOverview={{ marketplaces: [], browse, skills: [], total: 0 }}
        initialError={{ status: 401, message: loadErrorMessage(401, 'Skills') }}
      />
    );
  }

  const { tables } = await getPreferences(session.accessToken);
  const [marketplaces, page] = await Promise.all([
    loadMarketplaces(session.accessToken),
    loadSkillsPage(session.accessToken, browse, tables.defaultPageSize),
  ]);
  // With no Marketplaces the page shows that, not the skills request's 404.
  const listed = marketplaces.ok && marketplaces.data.length > 0;
  const failed = !marketplaces.ok ? marketplaces : listed && !page.ok ? page : null;

  return (
    <SkillsShell
      initialOverview={{
        marketplaces: marketplaces.ok ? marketplaces.data : [],
        browse,
        skills: page.ok ? page.data.skills : [],
        total: page.ok ? page.data.total : 0,
      }}
      initialError={failed && { status: failed.status, message: failed.message }}
    />
  );
}
