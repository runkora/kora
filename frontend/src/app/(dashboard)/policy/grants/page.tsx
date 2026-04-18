import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchGrants, fetchVocabulary } from '../_services/policy-server';
import { fetchPATAttribution, fetchUsersByIDs } from '../../model-gateway/_services/tokens-server';
import type {
  PATAttributionEntry,
  PATGrantAttribution,
} from '../../model-gateway/_services/tokens-server';
import { GrantsTab } from '../_components/grants-tab';

export default async function GrantsPage() {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return (
      <GrantsTab
        initialGrants={[]}
        availableScopes={[]}
        patAttribution={{}}
        userAttribution={{}}
        initialError="Not authenticated"
      />
    );
  }

  const [grantsResult, vocabResult] = await Promise.all([
    fetchGrants(session.accessToken, session.tenantId),
    fetchVocabulary(session.accessToken, session.tenantId),
  ]);

  const grants = grantsResult.ok ? (grantsResult.data.grants ?? []) : [];
  const scopes = vocabResult.ok ? (vocabResult.data.scopes ?? []).map((s) => s.scope) : [];
  const error = !grantsResult.ok
    ? grantsResult.message
    : !vocabResult.ok
      ? 'Failed to load available scopes — grant creation may be unavailable.'
      : null;

  // Resolve PAT and HUMAN principal IDs to user profiles in parallel.
  const patIds = grants.filter((g) => g.principal_type === 'PAT').map((g) => g.principal_id);
  const humanIds = grants.filter((g) => g.principal_type === 'HUMAN').map((g) => g.principal_id);

  const [attributionResult, usersResult] = await Promise.all([
    fetchPATAttribution(session.accessToken, patIds),
    fetchUsersByIDs(session.accessToken, humanIds),
  ]);

  const patAttribution: Record<string, PATGrantAttribution> = attributionResult.ok
    ? attributionResult.attribution
    : {};

  const userAttribution: Record<string, PATAttributionEntry> = {};
  if (usersResult.ok) {
    for (const u of usersResult.users) {
      userAttribution[u.id] = u;
    }
  }

  return (
    <GrantsTab
      initialGrants={grants}
      availableScopes={scopes}
      patAttribution={patAttribution}
      userAttribution={userAttribution}
      initialError={error}
    />
  );
}
