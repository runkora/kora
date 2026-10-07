// RSC: fetches the tenant's environments, the connection catalog, the
// registered sandbox images and the size limits server-side.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchEnvironments } from './_services/environments-server';
import { EnvironmentsShell } from './_components/environments-shell';

export default async function EnvironmentsPage() {
  const session = await getSession();
  const result = isSessionValid(session) ? await fetchEnvironments(session.accessToken) : null;

  const initialRows = result?.ok ? result.rows : [];
  const connections = result?.ok ? result.connections : [];
  const images = result?.ok ? result.images : null;
  const sizes = result?.ok ? result.sizes : null;
  const initialError = result && !result.ok ? result.message : null;

  return (
    <EnvironmentsShell
      initialRows={initialRows}
      connections={connections}
      images={images}
      sizes={sizes}
      initialError={initialError}
    />
  );
}
