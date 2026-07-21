import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchGrants, fetchVocabulary } from '../_services/policy-server';
import { fetchPATAttribution, fetchUsersByIDs } from '../../model-gateway/_services/tokens-server';
import type {
  PATAttributionEntry,
  PATGrantAttribution,
} from '../../model-gateway/_services/tokens-server';
import { GrantsTab } from '../_components/grants-tab';

const PAGE_LIMIT = 50;

interface Props {
  searchParams: Promise<{ page?: string }>;
}

export default async function GrantsPage({ searchParams }: Props) {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return (
      <GrantsTab
        initialGrants={[]}
        availableScopes={[]}
        patAttribution={{}}
        userAttribution={{}}
        initialError="Not authenticated"
        page={1}
        limit={PAGE_LIMIT}
        total={0}
      />
    );
  }

  const { page: pageParam } = await searchParams;
  const page = Math.max(1, parseInt(pageParam ?? '1', 10) || 1);

  const [grantsResult, vocabResult] = await Promise.all([
    fetchGrants(session.accessToken, session.tenantId, page, PAGE_LIMIT),
    fetchVocabulary(session.accessToken, session.tenantId),
  ]);

  const grants = grantsResult.ok ? (grantsResult.data.grants ?? []) : [];
  const total = grantsResult.ok ? (grantsResult.data.total ?? 0) : 0;
  const scopes = vocabResult.ok ? (vocabResult.data.scopes ?? []).map((s) => s.scope) : [];
  const error = !grantsResult.ok
    ? grantsResult.message
    : !vocabResult.ok
      ? 'Failed to load available scopes. Grant creation may be unavailable.'
      : null;

  // Resolve PAT and HUMAN principal IDs to user profiles in parallel.
  // With PAGE_LIMIT=50, IDs are safely under the backend's 100-ID cap.
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
      page={page}
      limit={PAGE_LIMIT}
      total={total}
    />
  );
}
