import { getSession, isSessionValid } from '@runkora/server/session';
import { loadErrorMessage } from '@runkora/lib/load-error';
import { fetchRoles, fetchVocabulary } from '../_services/policy-server';
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
  const scopes = vocabResult.ok ? (vocabResult.data.scopes ?? []).map((s) => s.scope) : [];
  // The vocabulary needs platform:grants:read, which a roles-only admin may lack.
  const error = !rolesResult.ok
    ? loadErrorMessage(rolesResult.status, 'roles')
    : !vocabResult.ok
      ? loadErrorMessage(vocabResult.status, 'the scope vocabulary')
      : null;

  return <RolesTab initialRoles={roles} availableScopes={scopes} initialError={error} />;
}
