// RSC — fetches initial analytics data, PATs, and personal quota server-side.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchAnalyticsMeFromGateway } from '../_services/analytics-server';
import { listPATsFromIdentity } from '../_services/tokens-server';
import { fetchPersonalQuotaFromGateway } from '../_services/quota-personal-server';
import { MyUsageShell } from './_components/my-usage-shell';

export default async function MyUsagePage() {
  const session = await getSession();

  if (!isSessionValid(session)) {
    return (
      <MyUsageShell
        initialData={null}
        initialError="Session expired — please sign in again."
        pats={[]}
        initialQuota={null}
      />
    );
  }

  const [analyticsResult, patsResult, quotaResult] = await Promise.all([
    fetchAnalyticsMeFromGateway(session.accessToken, {
      days: 30,
      granularity: 'daily',
    }),
    listPATsFromIdentity(session.accessToken),
    fetchPersonalQuotaFromGateway(session.accessToken),
  ]);

  return (
    <MyUsageShell
      initialData={analyticsResult.ok ? analyticsResult.data : null}
      initialError={analyticsResult.ok ? null : analyticsResult.message}
      pats={patsResult.ok ? patsResult.tokens : []}
      initialQuota={quotaResult.ok ? quotaResult.data : null}
    />
  );
}
