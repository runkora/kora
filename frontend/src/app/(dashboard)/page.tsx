// RSC — fetches agents, runs, connections, and environments in parallel so the
// dashboard renders with real data on first load.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchDashboardData } from './_services/dashboard-server';
import { DashboardPage } from './_components/dashboard-page';

export default async function DashboardRoute() {
  const session = await getSession();
  const data =
    isSessionValid(session)
      ? await fetchDashboardData(session.accessToken, session.tenantId)
      : { agentTypes: [], runs: [], connections: [], environments: [] };

  return <DashboardPage {...data} />;
}
