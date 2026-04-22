// RSC — fetches initial analytics data for the authenticated principal server-side.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchAnalyticsMeFromGateway } from '../_services/analytics-server';
import { MyUsageShell } from './_components/my-usage-shell';

export default async function MyUsagePage() {
  const session = await getSession();

  if (!isSessionValid(session)) {
    return (
      <MyUsageShell initialData={null} initialError="Session expired — please sign in again." />
    );
  }

  const result = await fetchAnalyticsMeFromGateway(session.accessToken, {
    days: 30,
    granularity: 'daily',
  });

  return (
    <MyUsageShell
      initialData={result.ok ? result.data : null}
      initialError={result.ok ? null : result.message}
    />
  );
}
