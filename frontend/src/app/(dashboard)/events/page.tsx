// RSC: loads the newest page of events so the page renders with real data. The live stream
// opens in the browser.

import { getSession, isSessionValid } from '@runkora/server/session';
import { EVENT_PAGE_SIZE, NO_FILTERS, filterParams } from './_types';
import { loadEventPage } from './_services/events-server';
import { EventsShell } from './_components/events-shell';

export default async function EventsRoute() {
  const session = await getSession();
  const key = filterParams(NO_FILTERS).toString();
  const query = new URLSearchParams(key);
  query.set('limit', String(EVENT_PAGE_SIZE));
  const result = isSessionValid(session)
    ? await loadEventPage(session.accessToken, query)
    : ({ ok: false, status: 401, message: 'Session expired. Sign in again.' } as const);

  return (
    <EventsShell
      initial={{
        key,
        page: result.ok ? result.data : null,
        error: result.ok ? null : result.message,
      }}
      traceUrlTemplate={process.env.KORA_TRACE_URL_TEMPLATE ?? ''}
    />
  );
}
