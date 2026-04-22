// RSC — fetches initial tenant-wide analytics data server-side.
// Requires platform:analytics:read scope; the policy enforces this at the API layer.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchAnalyticsFromGateway } from '../_services/analytics-server';
import { AnalyticsShell } from './_components/analytics-shell';

export default async function AnalyticsPage() {
  const session = await getSession();

  if (!isSessionValid(session)) {
    return (
      <AnalyticsShell initialData={null} initialError="Session expired — please sign in again." />
    );
  }

  const result = await fetchAnalyticsFromGateway(session.accessToken, {
    days: 30,
    granularity: 'daily',
  });

  return (
    <AnalyticsShell
      initialData={result.ok ? result.data : null}
      initialError={result.ok ? null : result.message}
    />
  );
}
