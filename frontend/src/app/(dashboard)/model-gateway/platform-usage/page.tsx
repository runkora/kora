// RSC — fetches aggregate platform-wide analytics server-side for the operator view.
// Requires platform:analytics:read scope; enforced at the API layer.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchAnalyticsFromGateway } from '../_services/analytics-server';
import { PlatformUsageShell } from './_components/platform-usage-shell';

export default async function PlatformUsagePage() {
  const session = await getSession();

  if (!isSessionValid(session)) {
    return (
      <PlatformUsageShell
        initialData={null}
        initialError="Session expired — please sign in again."
      />
    );
  }

  const analyticsResult = await fetchAnalyticsFromGateway(session.accessToken, {
    days: 30,
    granularity: 'daily',
  });

  return (
    <PlatformUsageShell
      initialData={analyticsResult.ok ? analyticsResult.data : null}
      initialError={analyticsResult.ok ? null : analyticsResult.message}
    />
  );
}
