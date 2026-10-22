// RSC — fetches PATs server-side so the page renders with real data.
// The co-located loading.tsx shows a skeleton while this fetch completes.

import { getSession, isSessionValid } from '@runkora/server/session';
import { listPATsFromIdentity } from '../_services/tokens-server';
import { loadScopeOptions } from '../_services/scope-options';
import { fetchEffectiveScopes, fetchVocabulary } from '../../policy/_services/policy-server';
import { fetchUsageStats } from '../_services/usage-server';
import { KeysTab } from '../_components/keys-tab';
import type { TokenUsageStat } from '../_services/usage-server';

export default async function KeysPage() {
  const session = await getSession();

  const expired = {
    ok: false as const,
    status: 401,
    message: 'Session expired. Please sign in again.',
  };
  const valid = isSessionValid(session);
  const accessToken = valid ? session.accessToken : '';
  const tenantId = valid ? session.tenantId : '';
  const scopesPromise = valid ? fetchEffectiveScopes(accessToken) : Promise.resolve(expired);
  // Descriptions and grouping come from the vocabulary, which only platform:grants:read may fetch.
  const scopeOptionsPromise = scopesPromise.then((r) =>
    loadScopeOptions(r.ok ? r.data.scopes : [], () => fetchVocabulary(accessToken, tenantId)),
  );
  const [tokensResult, scopesResult, statsResult, scopeOptions] = await Promise.all([
    valid ? listPATsFromIdentity(session.accessToken) : Promise.resolve(expired),
    scopesPromise,
    valid ? fetchUsageStats(session.accessToken) : Promise.resolve(expired),
    scopeOptionsPromise,
  ]);

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
      scopeOptions={scopeOptions}
      usageStats={usageStats}
    />
  );
}
