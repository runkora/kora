// RSC — fetches PATs server-side so the page renders with real data.
// The co-located loading.tsx shows a skeleton while this fetch completes.

import { getSession, isSessionValid } from '@orbit/server/session';
import { listPATsFromIdentity } from '../_services/tokens-server';
import { KeysTab } from '../_components/keys-tab';

export default async function KeysPage() {
  const session = await getSession();
  const result = isSessionValid(session)
    ? await listPATsFromIdentity(session.accessToken)
    : { ok: false as const, status: 401, message: 'Session expired — please sign in again.' };

  const initialTokens = result.ok ? result.tokens : [];
  const initialError = result.ok ? null : result.message;

  return <KeysTab initialTokens={initialTokens} initialError={initialError} />;
}
