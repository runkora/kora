// RSC — loads the caller's own dev environments server-side so the page
// renders with real data immediately.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchDevEnvironmentsPage } from './_services/dev-environments-server';
import { DevEnvironmentsShell } from './_components/dev-environments-shell';

export default async function DevEnvironmentsPageRoute() {
  const session = await getSession();
  const result = isSessionValid(session)
    ? await fetchDevEnvironmentsPage(session.accessToken)
    : null;

  return (
    <DevEnvironmentsShell
      environments={result?.ok ? result.environments : []}
      initialError={result && !result.ok ? result.message : null}
    />
  );
}
