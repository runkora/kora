import { getSession, isSessionValid } from '@runkora/server/session';
import { fetchGrants, fetchVocabulary } from '../_services/policy-server';
import { toScopeOption } from '@runkora/components/scope-option';
import { fetchPATAttribution } from '../../model-gateway/_services/tokens-server';
import { resolveUserDisplayNames } from '@runkora/server/services/identity-server';
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
  const vocabulary = vocabResult.ok ? (vocabResult.data.scopes ?? []) : [];
  const scopes = vocabulary.map((s) => s.scope);
  const scopeOptions = vocabulary.map(toScopeOption);
  const error = !grantsResult.ok
    ? grantsResult.message
    : !vocabResult.ok
      ? 'Failed to load available scopes. Grant creation may be unavailable.'
      : null;

  // Resolve PAT and HUMAN principal IDs to user profiles in parallel.
  // With PAGE_LIMIT=50, IDs are safely under the backend's 100-ID cap.
  const patIds = grants.filter((g) => g.principal_type === 'PAT').map((g) => g.principal_id);
  const humanIds = grants.filter((g) => g.principal_type === 'HUMAN').map((g) => g.principal_id);

  // Names come from the by-id lookup, which needs no directory scope (identity:users:read).
  const [attributionResult, userNames] = await Promise.all([
    fetchPATAttribution(session.accessToken, patIds),
    resolveUserDisplayNames(session.accessToken, humanIds),
  ]);

  const patAttribution: Record<string, PATGrantAttribution> = attributionResult.ok
    ? attributionResult.attribution
    : {};

  const userAttribution: Record<string, PATAttributionEntry> = {};
  for (const [id, displayName] of userNames) {
    userAttribution[id] = { id, email: '', display_name: displayName };
  }

  return (
    <GrantsTab
      initialGrants={grants}
      availableScopes={scopes}
      scopeOptions={scopeOptions}
      patAttribution={patAttribution}
      userAttribution={userAttribution}
      initialError={error}
      page={page}
      limit={PAGE_LIMIT}
      total={total}
    />
  );
}
