// RSC — fetches initial tenant-wide analytics data and model list server-side.
// Requires platform:analytics:read scope; the policy enforces this at the API layer.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchAnalyticsFromGateway } from '../_services/analytics-server';
import { fetchModelsFromGateway } from '../_services/models-server';
import { getPreferences } from '../../_preferences/preferences-server';
import { toDays } from '../../_preferences/period';
import { AnalyticsShell } from './_components/analytics-shell';

export default async function AnalyticsPage() {
  const session = await getSession();

  if (!isSessionValid(session)) {
    return (
      <AnalyticsShell
        initialData={null}
        initialError="Session expired — please sign in again."
        models={[]}
      />
    );
  }

  const preferences = await getPreferences(session.accessToken);
  const days = toDays(preferences.analytics.defaultPeriod);

  const [analyticsResult, modelsResult] = await Promise.all([
    fetchAnalyticsFromGateway(session.accessToken, {
      days,
      granularity: 'daily',
    }),
    fetchModelsFromGateway(session.accessToken),
  ]);

  return (
    <AnalyticsShell
      initialData={analyticsResult.ok ? analyticsResult.data : null}
      initialError={analyticsResult.ok ? null : analyticsResult.message}
      models={modelsResult.ok ? modelsResult.models : []}
    />
  );
}
