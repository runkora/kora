// RSC — loads webhook sources, triggers, recent deliveries, and the tenant's
// agent types (for the trigger form) server-side so the page renders with real
// data immediately.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchTriggersPage, SESSION_EXPIRED_MESSAGE } from './_services/triggers-server';
import { TriggersShell } from './_components/triggers-shell';

export default async function TriggersPageRoute() {
  const session = await getSession();
  // An invalid session says so. Rendering the empty shell would claim this
  // tenant has no triggers, which is a different and much worse statement than
  // "we could not ask".
  const result = isSessionValid(session)
    ? await fetchTriggersPage(session.accessToken)
    : ({ ok: false, message: SESSION_EXPIRED_MESSAGE } as const);

  return (
    <TriggersShell
      sources={result.ok ? result.sources : []}
      triggers={result.ok ? result.triggers : []}
      deliveries={result.ok ? result.deliveries : []}
      initialDeliveryCursor={result.ok ? result.deliveryCursor : null}
      agents={result.ok ? result.agents : []}
      initialError={result.ok ? null : result.message}
    />
  );
}
