import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchScopesExplain, fetchVocabulary, fetchRoles } from '../_services/policy-server';
import { EvaluateTab } from '../_components/evaluate-tab';

export default async function EvaluatePage() {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return (
      <EvaluateTab
        initialExplain={null}
        availableScopes={[]}
        availableRoles={[]}
        initialError="Not authenticated"
      />
    );
  }

  const [explainResult, vocabResult, rolesResult] = await Promise.all([
    fetchScopesExplain(session.accessToken),
    fetchVocabulary(session.accessToken, session.tenantId),
    fetchRoles(session.accessToken, session.tenantId),
  ]);

  const explain = explainResult.ok ? explainResult.data : null;
  const scopes = vocabResult.ok ? (vocabResult.data.scopes ?? []).map((s) => s.scope) : [];
  const roles = rolesResult.ok ? (rolesResult.data.roles ?? []).map((r) => r.role) : [];
  const error = !explainResult.ok ? explainResult.message : null;

  return (
    <EvaluateTab
      initialExplain={explain}
      availableScopes={scopes}
      availableRoles={roles}
      initialError={error}
    />
  );
}
