import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchGrants, fetchVocabulary } from '../_services/policy-server';
import { GrantsTab } from '../_components/grants-tab';

export default async function GrantsPage() {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return <GrantsTab initialGrants={[]} availableScopes={[]} initialError="Not authenticated" />;
  }

  const [grantsResult, vocabResult] = await Promise.all([
    fetchGrants(session.accessToken, session.tenantId),
    fetchVocabulary(session.accessToken, session.tenantId),
  ]);

  const grants = grantsResult.ok ? (grantsResult.data.grants ?? []) : [];
  const scopes = vocabResult.ok ? (vocabResult.data.scopes ?? []).map((s) => s.scope) : [];
  const error = !grantsResult.ok ? grantsResult.message : null;

  return <GrantsTab initialGrants={grants} availableScopes={scopes} initialError={error} />;
}
