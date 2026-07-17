// RSC — fetches initial analytics data, PATs, and personal budget server-side.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchAnalyticsMeFromGateway } from '../_services/analytics-server';
import { listPATsFromIdentity } from '../_services/tokens-server';
import { fetchPersonalBudgetFromGateway } from '../_services/budget-personal-server';
import { getPreferences } from '../../_preferences/preferences-server';
import { toDays } from '../../_preferences/period';
import { MyUsageShell } from './_components/my-usage-shell';

export default async function MyUsagePage() {
  const session = await getSession();

  if (!isSessionValid(session)) {
    return (
      <MyUsageShell
        initialData={null}
        initialError="Session expired — please sign in again."
        pats={[]}
        initialBudget={null}
      />
    );
  }

  const preferences = await getPreferences(session.accessToken);
  const days = toDays(preferences.myUsage.defaultPeriod);

  const [analyticsResult, patsResult, budgetResult] = await Promise.all([
    fetchAnalyticsMeFromGateway(session.accessToken, {
      days,
      granularity: 'daily',
    }),
    listPATsFromIdentity(session.accessToken),
    fetchPersonalBudgetFromGateway(session.accessToken),
  ]);

  return (
    <MyUsageShell
      initialData={analyticsResult.ok ? analyticsResult.data : null}
      initialError={analyticsResult.ok ? null : analyticsResult.message}
      pats={patsResult.ok ? patsResult.tokens : []}
      initialBudget={budgetResult.ok ? budgetResult.data : null}
    />
  );
}
