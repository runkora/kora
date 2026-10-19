// RSC — fetches initial analytics data, PATs, and personal budget server-side.

import { getSession, isSessionValid } from '@runkora/server/session';
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
        initialError="Session expired. Please sign in again."
        pats={[]}
        initialBudget={null}
      />
    );
  }

  // Only the analytics query depends on preferences (the default period), so
  // the PAT and budget fetches start immediately rather than waiting behind it.
  const patsPromise = listPATsFromIdentity(session.accessToken);
  const budgetPromise = fetchPersonalBudgetFromGateway(session.accessToken);
  const preferences = await getPreferences(session.accessToken);
  const days = toDays(preferences.myUsage.defaultPeriod);

  const [analyticsResult, patsResult, budgetResult] = await Promise.all([
    fetchAnalyticsMeFromGateway(session.accessToken, {
      days,
      granularity: 'daily',
    }),
    patsPromise,
    budgetPromise,
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
