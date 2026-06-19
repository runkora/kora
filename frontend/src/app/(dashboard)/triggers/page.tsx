// RSC — loads webhook sources, triggers, recent deliveries, and the tenant's
// agent types (for the trigger form) server-side so the page renders with real
// data immediately.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchTriggersPage } from './_services/triggers-server';
import { TriggersShell } from './_components/triggers-shell';

export default async function TriggersPageRoute() {
  const session = await getSession();
  const result = isSessionValid(session) ? await fetchTriggersPage(session.accessToken) : null;

  return (
    <TriggersShell
      sources={result?.ok ? result.sources : []}
      triggers={result?.ok ? result.triggers : []}
      deliveries={result?.ok ? result.deliveries : []}
      agents={result?.ok ? result.agents : []}
      initialError={result && !result.ok ? result.message : null}
    />
  );
}
