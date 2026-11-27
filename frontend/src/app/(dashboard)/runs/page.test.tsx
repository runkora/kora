/** Unit tests for the Runs route's server read of the first page. */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { getSession, isSessionValid } from '@runkora/server/session';
import { listRunsWithNames } from '@runkora/server/services/runs-server';
import type { InitialRunsPage } from './_hooks/use-runs-feed';
import { runFilterParams, runFiltersFromUrl } from './_types';
import RunsRoute from './page';

vi.mock('@runkora/server/session', () => ({ getSession: vi.fn(), isSessionValid: vi.fn() }));
vi.mock('@runkora/server/services/runs-server', () => ({
  listRunsWithNames: vi.fn(),
  pickRunsPageParams: (params: URLSearchParams) => params,
}));
vi.mock('./_components/runs-page', () => ({ RunsPage: () => null }));

/** The first page the route hands to the Runs page for the URL's params. */
async function initialPageFor(params: Record<string, string>): Promise<InitialRunsPage | null> {
  const element = await RunsRoute({ searchParams: Promise.resolve(params) });
  return (element.props as { initialPage: InitialRunsPage | null }).initialPage;
}

beforeEach(() => {
  vi.mocked(isSessionValid).mockReturnValue(true);
  vi.mocked(getSession).mockResolvedValue({ accessToken: 'token' } as Awaited<
    ReturnType<typeof getSession>
  >);
});

afterEach(() => {
  vi.useRealTimers();
  vi.mocked(listRunsWithNames).mockReset();
});

describe('RunsRoute', () => {
  it('reads the first page for the URL filters under the caller session', async () => {
    vi.mocked(listRunsWithNames).mockResolvedValue({ data: [], next_cursor: 'cur1' });

    const page = await initialPageFor({ quick: 'failed' });

    const key = runFilterParams(runFiltersFromUrl(new URLSearchParams('quick=failed'))).toString();
    expect(page).toMatchObject({ key, page: { runs: [], nextCursor: 'cur1' } });
    const [token, params] = vi.mocked(listRunsWithNames).mock.calls[0]!;
    expect(token).toBe('token');
    expect(params.get('limit')).toBe('50');
    expect(params.get('state')).toBe(new URLSearchParams(key).get('state'));
  });

  it('reads nothing for an invalid session', async () => {
    vi.mocked(isSessionValid).mockReturnValue(false);

    expect(await initialPageFor({})).toBeNull();
    expect(listRunsWithNames).not.toHaveBeenCalled();
  });

  it('renders without a page when the read fails', async () => {
    vi.mocked(listRunsWithNames).mockRejectedValue(new Error('upstream'));

    expect(await initialPageFor({})).toBeNull();
  });

  it('gives up on a slow read at the bound and cancels it', async () => {
    vi.useFakeTimers();
    vi.mocked(listRunsWithNames).mockReturnValue(new Promise(() => {}));

    const pending = initialPageFor({});
    await vi.advanceTimersByTimeAsync(1_500);

    expect(await pending).toBeNull();
    const signal = vi.mocked(listRunsWithNames).mock.calls[0]![2];
    expect(signal?.aborted).toBe(true);
  });
});
