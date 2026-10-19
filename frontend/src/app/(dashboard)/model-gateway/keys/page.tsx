// RSC — fetches PATs server-side so the page renders with real data.
// The co-located loading.tsx shows a skeleton while this fetch completes.

import { getSession, isSessionValid } from '@runkora/server/session';
import { listPATsFromIdentity } from '../_services/tokens-server';
import { fetchEffectiveScopes } from '../../policy/_services/policy-server';
import { fetchUsageStats } from '../_services/usage-server';
import { KeysTab } from '../_components/keys-tab';
import type { TokenUsageStat } from '../_services/usage-server';

export default async function KeysPage() {
  const session = await getSession();

  const [tokensResult, scopesResult, statsResult] = isSessionValid(session)
    ? await Promise.all([
        listPATsFromIdentity(session.accessToken),
        fetchEffectiveScopes(session.accessToken),
        fetchUsageStats(session.accessToken),
      ])
    : [
        { ok: false as const, status: 401, message: 'Session expired. Please sign in again.' },
        { ok: false as const, status: 401, message: 'Session expired. Please sign in again.' },
        { ok: false as const, status: 401, message: 'Session expired. Please sign in again.' },
      ];

  const initialTokens = tokensResult.ok ? tokensResult.tokens : [];
  const initialError = tokensResult.ok ? null : tokensResult.message;
  const effectiveScopes = scopesResult.ok ? scopesResult.data.scopes : [];
  if (!statsResult.ok) {
    console.error('[KeysPage] failed to load usage stats:', statsResult.message);
  }
  const usageStats: TokenUsageStat[] = statsResult.ok ? statsResult.stats : [];

  return (
    <KeysTab
      initialTokens={initialTokens}
      initialError={initialError}
      effectiveScopes={effectiveScopes}
      usageStats={usageStats}
    />
  );
}
