// RSC — fetches PATs server-side so the page renders with real data.
// The co-located loading.tsx shows a skeleton while this fetch completes.

import { getSession, isSessionValid } from '@orbit/server/session';
import { listPATsFromIdentity } from '../_services/tokens-server';
import { KeysTab } from '../_components/keys-tab';

export default async function KeysPage() {
  const session = await getSession();
  const result = isSessionValid(session) ? await listPATsFromIdentity(session.accessToken) : null;
  const initialTokens = result?.ok ? result.tokens : [];
  const initialError = result && !result.ok ? result.message : null;

  return <KeysTab initialTokens={initialTokens} initialError={initialError} />;
}
