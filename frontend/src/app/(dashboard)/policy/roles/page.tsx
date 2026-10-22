import { getSession, isSessionValid } from '@runkora/server/session';
import { loadErrorMessage } from '@runkora/lib/load-error';
import { fetchRoles, fetchVocabulary } from '../_services/policy-server';
import { toScopeOption } from '@runkora/components/scope-option';
import { RolesTab } from '../_components/roles-tab';

export default async function RolesPage() {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return <RolesTab initialRoles={[]} availableScopes={[]} initialError="Not authenticated" />;
  }

  const [rolesResult, vocabResult] = await Promise.all([
    fetchRoles(session.accessToken, session.tenantId),
    fetchVocabulary(session.accessToken, session.tenantId),
  ]);

  const roles = rolesResult.ok ? (rolesResult.data.roles ?? []) : [];
  const vocabulary = vocabResult.ok ? (vocabResult.data.scopes ?? []) : [];
  const scopes = vocabulary.map((s) => s.scope);
  const scopeOptions = vocabulary.map(toScopeOption);
  const error = rolesResult.ok ? null : loadErrorMessage(rolesResult.status, 'roles');
  // The vocabulary needs platform:grants:read, which a roles-only admin may lack.
  const scopesError = vocabResult.ok
    ? null
    : loadErrorMessage(vocabResult.status, 'the scope vocabulary');

  return (
    <RolesTab
      initialRoles={roles}
      availableScopes={scopes}
      scopeOptions={scopeOptions}
      scopesError={scopesError}
      initialError={error}
    />
  );
}
