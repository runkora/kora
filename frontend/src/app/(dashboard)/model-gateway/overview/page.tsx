import { getSession, isSessionValid } from '@runkora/server/session';
import { fetchAnalyticsFromGateway } from '../_services/analytics-server';
import { fetchModelsFromGateway } from '../_services/models-server';
import { fetchUsageFromGateway } from '../_services/usage-server';
import { OverviewTab } from '../_components/overview-tab';
import { ScopeGuard } from '../../_components';

export default async function OverviewPage() {
  const session = await getSession();

  if (!isSessionValid(session)) {
    return (
      <ScopeGuard scope="platform:analytics:read">
        <OverviewTab models={[]} data={null} recentRecords={[]} />
      </ScopeGuard>
    );
  }

  const [analyticsResult, modelsResult, usageResult] = await Promise.all([
    fetchAnalyticsFromGateway(session.accessToken, { days: 30, granularity: 'daily' }),
    fetchModelsFromGateway(session.accessToken),
    fetchUsageFromGateway(session.accessToken, { limit: 8 }),
  ]);

  return (
    <ScopeGuard scope="platform:analytics:read">
      <OverviewTab
        models={modelsResult.ok ? modelsResult.models : []}
        data={
          analyticsResult.ok
            ? {
                kpis: analyticsResult.data.kpis,
                buckets: analyticsResult.data.buckets,
                costByModel: analyticsResult.data.cost_by_model,
              }
            : null
        }
        recentRecords={usageResult.ok ? usageResult.records : []}
      />
    </ScopeGuard>
  );
}
