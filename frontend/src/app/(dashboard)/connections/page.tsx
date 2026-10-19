// RSC — fetches the read-only connection catalog and computes per-connection
// agent usage server-side so the page renders with real data immediately.

import { getSession, isSessionValid } from '@runkora/server/session';
import { fetchConnections } from './_services/connections-server';
import { ConnectionsShell } from './_components/connections-shell';

export default async function ConnectionsPage() {
  const session = await getSession();
  const result = isSessionValid(session)
    ? await fetchConnections(session.accessToken, session.tenantId)
    : null;

  const initialRows = result?.ok ? result.rows : [];
  const initialError = result && !result.ok ? result.message : null;

  return <ConnectionsShell initialRows={initialRows} initialError={initialError} />;
}
