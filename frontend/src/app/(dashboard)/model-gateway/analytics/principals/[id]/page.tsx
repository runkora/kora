// RSC — admin drill-down: fetches the full usage breakdown for a single
// principal server-side (GET /v1/analytics/principals/{id} via the BFF).
// Requires platform:analytics:read; the policy enforces this at the API layer.
// The principal's display_name/email are carried as query params from the
// Principals table link (already resolved there via user_profiles) rather
// than re-resolved here — see docs/adr/2026-04-27-inference-analytics.md
// (amended 2026-07-20).

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchAnalyticsPrincipalByIdFromGateway } from '../../../_services/analytics-server';
import { getPreferences } from '../../../../_preferences/preferences-server';
import { toDays } from '../../../../_preferences/period';
import { PrincipalUsageView } from './_components/principal-usage-view';

interface PrincipalUsagePageProps {
  params: Promise<{ id: string }>;
  searchParams: Promise<{ display_name?: string; email?: string }>;
}

export default async function PrincipalUsagePage({
  params,
  searchParams,
}: PrincipalUsagePageProps) {
  const { id } = await params;
  const { display_name: displayName, email } = await searchParams;
  const session = await getSession();

  if (!isSessionValid(session)) {
    return (
      <PrincipalUsageView
        principalId={id}
        displayName={displayName ?? null}
        email={email ?? null}
        initialData={null}
        initialError="Session expired — please sign in again."
      />
    );
  }

  const preferences = await getPreferences(session.accessToken);
  const days = toDays(preferences.analytics.defaultPeriod);

  const result = await fetchAnalyticsPrincipalByIdFromGateway(session.accessToken, id, {
    days,
    granularity: 'daily',
  });

  return (
    <PrincipalUsageView
      principalId={id}
      displayName={displayName ?? null}
      email={email ?? null}
      initialData={result.ok ? result.data : null}
      initialError={result.ok ? null : result.message}
    />
  );
}
