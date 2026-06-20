// RSC — fetches agents, runs, connections, environments, inference analytics, and
// the tenant budget in parallel so the dashboard renders with real data on first load.

import { getSession, isSessionValid } from '@orbit/server/session';
import {
  fetchDashboardData,
  ANALYTICS_WINDOW_DAYS,
  EMPTY_DASHBOARD_DATA,
} from './_services/dashboard-server';
import { DashboardPage } from './_components/dashboard-page';

export default async function DashboardRoute() {
  const session = await getSession();
  const data = isSessionValid(session)
    ? await fetchDashboardData(session.accessToken, session.tenantId)
    : EMPTY_DASHBOARD_DATA;

  return <DashboardPage {...data} windowDays={ANALYTICS_WINDOW_DAYS} />;
}
