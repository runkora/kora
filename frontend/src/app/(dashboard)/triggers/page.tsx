// RSC: loads webhook sources, triggers with their stats over the `?window=`
// range, and the tenant's agent types (for the trigger form) server-side so the
// page renders with real data immediately.

import { Suspense } from 'react';
import { getSession, isSessionValid } from '@runkora/server/session';
import { fetchTriggersPage, SESSION_EXPIRED_MESSAGE } from './_services/triggers-server';
import { TriggersShell } from './_components/triggers-shell';
import { parseStatsRange, statsWindowOf } from './_types/stats';
import type { StatsRange } from './_types/stats';
import TriggersLoading from './loading';

interface TriggersPageRouteProps {
  searchParams: Promise<{ window?: string }>;
}

export default async function TriggersPageRoute({ searchParams }: TriggersPageRouteProps) {
  const range = parseStatsRange((await searchParams).window);
  // Keyed on the range: a new range shows the loading state until its stats land.
  return (
    <Suspense key={range} fallback={<TriggersLoading />}>
      <TriggersContent range={range} />
    </Suspense>
  );
}

async function TriggersContent({ range }: { range: StatsRange }) {
  const session = await getSession();
  // An invalid session says so. Rendering the empty shell would claim this
  // tenant has no triggers, which is a different and much worse statement than
  // "we could not ask".
  const result = isSessionValid(session)
    ? await fetchTriggersPage(session.accessToken, statsWindowOf(range))
    : ({ ok: false, message: SESSION_EXPIRED_MESSAGE } as const);

  return (
    <TriggersShell
      sources={result.ok ? result.sources : []}
      triggers={result.ok ? result.triggers : []}
      stats={result.ok ? result.stats : null}
      range={range}
      agents={result.ok ? result.agents : []}
      agentsError={result.ok ? result.agentsError : null}
      initialError={result.ok ? null : result.message}
    />
  );
}
