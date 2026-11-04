import { getSession, isSessionValid } from '@runkora/server/session';
import {
  fetchStats,
  fetchGrants,
  fetchRoles,
  fetchVocabulary,
  fetchRoutes,
} from '../_services/policy-server';
import { OverviewTab } from '../_components/overview-tab';

export default async function PolicyOverviewPage() {
  const session = await getSession();
  if (!isSessionValid(session)) {
    return <OverviewTab stats={null} recentGrants={[]} roles={[]} vocabulary={[]} routes={[]} />;
  }

  const [statsResult, grantsResult, rolesResult, vocabResult, routesResult] = await Promise.all([
    fetchStats(session.accessToken, session.tenantId),
    fetchGrants(session.accessToken, session.tenantId),
    fetchRoles(session.accessToken, session.tenantId),
    fetchVocabulary(session.accessToken, session.tenantId),
    fetchRoutes(session.accessToken),
  ]);

  const stats = statsResult?.ok ? statsResult.data : null;
  const grants = grantsResult?.ok ? (grantsResult.data.grants ?? []) : [];
  const roles = rolesResult?.ok ? (rolesResult.data.roles ?? []) : [];
  const vocabulary = vocabResult?.ok ? (vocabResult.data.scopes ?? []) : [];
  const routes = routesResult?.ok ? (routesResult.data.registrations ?? []) : [];

  // Most recent grants first
  const recentGrants = [...grants]
    .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
    .slice(0, 5);

  return (
    <OverviewTab
      stats={stats}
      recentGrants={recentGrants}
      roles={roles}
      vocabulary={vocabulary}
      routes={routes}
    />
  );
}
