import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchStats } from '../_services/policy-server';
import { OverviewTab } from '../_components/overview-tab';

export default async function PolicyOverviewPage() {
  const session = await getSession();
  const result = isSessionValid(session)
    ? await fetchStats(session.accessToken, session.tenantId)
    : null;
  const stats = result?.ok ? result.data : null;

  return <OverviewTab stats={stats} />;
}
