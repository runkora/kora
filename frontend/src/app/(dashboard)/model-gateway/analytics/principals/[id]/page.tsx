// RSC — admin drill-down: fetches the full usage breakdown for a single
// principal server-side (GET /v1/analytics/principals/{id} via the BFF).
// Requires platform:analytics:read; the policy enforces this at the API layer.
//
// display_name/email are resolved server-side here via fetchUsersByIDs, keyed
// off the trusted `id` path param — not trusted from client-supplied query
// params. Query params were spoofable (anyone could edit the URL to show an
// arbitrary name/email for a given principal_id) and leaked PII (email) into
// the URL, browser history, and server logs. See
// docs/adr/2026-04-27-inference-analytics.md (amended 2026-07-20).

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchAnalyticsPrincipalByIdFromGateway } from '../../../_services/analytics-server';
import { fetchUsersByIDs } from '../../../_services/tokens-server';
import { getPreferences } from '../../../../_preferences/preferences-server';
import { toDays } from '../../../../_preferences/period';
import { PrincipalUsageView } from './_components/principal-usage-view';

interface PrincipalUsagePageProps {
  params: Promise<{ id: string }>;
}

export default async function PrincipalUsagePage({ params }: PrincipalUsagePageProps) {
  const { id } = await params;
  const session = await getSession();

  if (!isSessionValid(session)) {
    return (
      <PrincipalUsageView
        principalId={id}
        displayName={null}
        email={null}
        initialData={null}
        initialError="Session expired — please sign in again."
        notFound={false}
      />
    );
  }

  const preferences = await getPreferences(session.accessToken);
  const days = toDays(preferences.analytics.defaultPeriod);

  // Resolve identity and usage in parallel — independent calls, no data
  // dependency between them.
  const [result, usersResult] = await Promise.all([
    fetchAnalyticsPrincipalByIdFromGateway(session.accessToken, id, {
      days,
      granularity: 'daily',
    }),
    fetchUsersByIDs(session.accessToken, [id]),
  ]);

  // Non-human principals (service accounts, PATs) won't resolve to an
  // identity user — displayName/email stay null and the view falls back to
  // principalId, same as before.
  const user = usersResult.ok ? usersResult.users.find((u) => u.id === id) : undefined;

  // The KPI query uses COALESCE(SUM(...), 0) with no GROUP BY, so a
  // principal_id with zero matching inference_usage rows still returns 200
  // with an all-zero response — not a 404. That's indistinguishable from a
  // real, quiet principal unless we also check identity resolution.
  //
  // notFound heuristic: treat the principal as unresolvable only when BOTH
  // signals are empty — no identity match AND no usage ever in this window.
  // This correctly handles the common cases (human user with/without usage;
  // service account/PAT with usage, which never resolves via identity but
  // has real data to show) at the cost of one edge case: a service
  // account/PAT/agent that is valid but has never made a single request will
  // also show as "not found." Distinguishing that from a truly bogus ID
  // would require a canonical all-principal-types registry, which doesn't
  // exist today.
  const hasNoUsage = result.ok && result.data.kpis.total_requests === 0;
  const notFound = !user && hasNoUsage;

  return (
    <PrincipalUsageView
      principalId={id}
      displayName={user?.display_name ?? null}
      email={user?.email ?? null}
      initialData={result.ok ? result.data : null}
      initialError={result.ok ? null : result.message}
      notFound={result.ok && notFound}
    />
  );
}
