import { getSession, isSessionValid } from '@runkora/server/session';
import { fetchVocabulary } from '../_services/policy-server';
import { VocabularyTab } from '../_components/vocabulary-tab';

export default async function VocabularyPage() {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return <VocabularyTab initialScopes={[]} initialError="Not authenticated" />;
  }

  const result = await fetchVocabulary(session.accessToken, session.tenantId);
  const scopes = result.ok ? (result.data.scopes ?? []) : [];
  const error = !result.ok ? result.message : null;

  return <VocabularyTab initialScopes={scopes} initialError={error} />;
}
