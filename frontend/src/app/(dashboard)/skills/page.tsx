import { getSession, isSessionValid } from '@runkora/server/session';
import { loadMarketplacePage } from './_services/skills-server';
import { MarketplaceShell } from './_components/skills-shell';

export default async function MarketplaceEntryPage() {
  const session = await getSession();
  const result = await loadMarketplacePage(isSessionValid(session) ? session.accessToken : null);

  return (
    <MarketplaceShell
      key=""
      initialOverview={result.overview}
      initialError={result.ok ? null : result.message}
    />
  );
}
