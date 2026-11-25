// Client-side calls to the run list BFF routes. Never calls kora-agents directly.

import { loadErrorMessage } from '@runkora/lib/load-error';
import { ApiRoute } from '@runkora/lib/routing/api-routes';
import type { RunsPageResponse } from '@runkora/shared/api';
import type { FeedPage } from '../_lib/runs-feed';

/** One page of runs for the query. Returns the error as a sentence, never throws. */
export async function fetchRunsPage(
  params: URLSearchParams,
): Promise<{ page: FeedPage | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(`${ApiRoute.runs.list()}?${params.toString()}`, { cache: 'no-store' });
  } catch {
    return { page: null, error: 'Network error: could not reach the server.' };
  }
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { message?: string };
    return { page: null, error: loadErrorMessage(res.status, 'runs', body.message) };
  }
  try {
    const body = (await res.json()) as RunsPageResponse;
    return { page: { runs: body.data ?? [], nextCursor: body.next_cursor ?? null }, error: null };
  } catch {
    return { page: null, error: 'The server sent a response the page could not read.' };
  }
}

/** Opens the live run list for the filters; `after` in the params resumes past that seq. */
export function openRunsStream(params: URLSearchParams): EventSource {
  return new EventSource(ApiRoute.runs.stream(params));
}
