// The Runs tab. The first page for the URL's filters is read here, under the caller's
// session, so the rows arrive with the page. The browser still opens the live stream and
// reads the first page again once it is subscribed, which replaces this one.

import { getSession, isSessionValid } from '@runkora/server/session';
import { listRunsWithNames, pickRunsPageParams } from '@runkora/server/services/runs-server';
import { RunsPage } from './_components/runs-page';
import type { InitialRunsPage } from './_hooks/use-runs-feed';
import { RUNS_PAGE_SIZE, runFilterParams, runFiltersFromUrl } from './_types';

interface RunsRouteProps {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}

export default async function RunsRoute({ searchParams }: RunsRouteProps) {
  return <RunsPage initialPage={await readFirstPage(await searchParams)} />;
}

/** The first page for the filters in the URL, or null when it cannot be read here. */
async function readFirstPage(
  searchParams: Record<string, string | string[] | undefined>,
): Promise<InitialRunsPage | null> {
  const session = await getSession();
  if (!isSessionValid(session)) return null;
  const url = new URLSearchParams();
  for (const [k, v] of Object.entries(searchParams)) if (typeof v === 'string') url.set(k, v);
  const key = runFilterParams(runFiltersFromUrl(url)).toString();
  const query = new URLSearchParams(key);
  query.set('limit', String(RUNS_PAGE_SIZE));
  try {
    const body = await listRunsWithNames(session.accessToken, pickRunsPageParams(query));
    return {
      id: crypto.randomUUID(),
      key,
      page: { runs: body.data ?? [], nextCursor: body.next_cursor ?? null },
    };
  } catch {
    // The browser reads the page itself.
    return null;
  }
}
