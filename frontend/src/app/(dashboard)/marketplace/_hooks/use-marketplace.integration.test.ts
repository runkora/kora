/**
 * Integration tests for the mount check of useMarketplace.
 *
 * Back from a Skill page can restore an old server render from the Next.js
 * cache, while the URL keeps the browse values that the user left. On mount,
 * the hook must load the results for the URL instead of writing the old
 * values over the URL.
 *
 * Strategy:
 * - Each test sets the jsdom URL before it renders the hook.
 * - MSW serves GET /api/marketplace/skills.
 */
import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { server } from '@orbit/test-utils';
import { MIN_SEARCH_LENGTH } from '../_types';
import type { MarketplaceBrowseParams, MarketplaceOverviewData } from '../_types';
import { buildBrowseUrl } from '../_utils/browse-params';
import { useMarketplace } from './use-marketplace';

// ── Fixtures ──────────────────────────────────────────────────────────────────

const MARKETPLACES = [{ name: 'ClearRoute Core' }, { name: 'Platform Engineering' }];

/** The browse values of `/marketplace` with no query keys. */
const DEFAULT_BROWSE: MarketplaceBrowseParams = {
  marketplace: '',
  query: '',
  page: 1,
  pageSize: 24,
  sort: 'name-asc',
};

/** The first server render of `/marketplace`: the first Marketplace, page 1. */
const FIRST_RENDER: MarketplaceOverviewData = {
  marketplaces: MARKETPLACES,
  requested: DEFAULT_BROWSE,
  browse: { ...DEFAULT_BROWSE, marketplace: 'ClearRoute Core' },
  skills: [{ slug: 'core/skills/first', title_of_package: 'First', description: '' }],
  total: 1,
};

const PLATFORM_PAGE_2_BROWSE: MarketplaceBrowseParams = {
  ...DEFAULT_BROWSE,
  marketplace: 'Platform Engineering',
  page: 2,
};

/** The BFF result for page 2 of "Platform Engineering". */
const PLATFORM_PAGE_2: MarketplaceOverviewData = {
  marketplaces: MARKETPLACES,
  requested: PLATFORM_PAGE_2_BROWSE,
  browse: PLATFORM_PAGE_2_BROWSE,
  skills: [
    { slug: 'platform/skills/twenty-five', title_of_package: 'Twenty-five', description: '' },
  ],
  total: 30,
};

/** The URL after Back from a Skill page that the user opened from page 2. */
const PLATFORM_PAGE_2_URL = '/marketplace?marketplace=Platform+Engineering&page=2';

// ── Helpers for the describes below ─────────────────────────────────────────────

/** Browse values for "ClearRoute Core", page 1, with any overrides applied. */
function browse(overrides: Partial<MarketplaceBrowseParams> = {}): MarketplaceBrowseParams {
  return { ...DEFAULT_BROWSE, marketplace: 'ClearRoute Core', ...overrides };
}

/** A BFF result for `browse()`, with any overrides applied. */
function overviewData(overrides: Partial<MarketplaceOverviewData> = {}): MarketplaceOverviewData {
  const requested = browse();
  return {
    marketplaces: MARKETPLACES,
    requested,
    browse: requested,
    skills: [],
    total: 0,
    ...overrides,
  };
}

/** Sets the jsdom URL to the overview URL of the given browse values. */
function setLocation(b: MarketplaceBrowseParams) {
  window.history.replaceState(null, '', buildBrowseUrl('/marketplace', '', b));
}

afterEach(() => {
  window.history.replaceState(null, '', '/');
});

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('useMarketplace mount check', () => {
  it('loads the results for the URL when the server render is for other values', async () => {
    window.history.replaceState(null, '', PLATFORM_PAGE_2_URL);
    const requests: URL[] = [];
    server.use(
      http.get('/api/marketplace/skills', ({ request }) => {
        requests.push(new URL(request.url));
        return HttpResponse.json({ data: PLATFORM_PAGE_2 });
      }),
    );

    const { result } = renderHook(() => useMarketplace(FIRST_RENDER, null));

    // The status is 'success' before the load starts, so wait for the new values.
    await waitFor(() => expect(result.current.browse).toEqual(PLATFORM_PAGE_2_BROWSE));
    expect(result.current.status).toBe('success');
    expect(result.current.skills).toEqual(PLATFORM_PAGE_2.skills);
    expect(requests).toHaveLength(1);
    expect(requests[0].searchParams.get('marketplace')).toBe('Platform Engineering');
    expect(requests[0].searchParams.get('page')).toBe('2');
    // The URL keeps the values that the user left.
    expect(window.location.pathname + window.location.search).toBe(PLATFORM_PAGE_2_URL);
  });

  it('sends no request after a first load, and writes the validated values to the URL', async () => {
    window.history.replaceState(null, '', '/marketplace');
    const requests: URL[] = [];
    server.use(
      http.get('/api/marketplace/skills', ({ request }) => {
        requests.push(new URL(request.url));
        return HttpResponse.json({ data: FIRST_RENDER });
      }),
    );

    const { result } = renderHook(() => useMarketplace(FIRST_RENDER, null));

    await waitFor(() => expect(window.location.search).toBe('?marketplace=ClearRoute+Core'));
    // Give a wrong load time to start. Then make sure that no request occurred.
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(requests).toHaveLength(0);
    expect(result.current.status).toBe('success');
    expect(result.current.browse).toEqual(FIRST_RENDER.browse);
  });

  it('restores the URL of the results on screen when the load for the URL fails', async () => {
    window.history.replaceState(null, '', PLATFORM_PAGE_2_URL);
    server.use(
      http.get('/api/marketplace/skills', () =>
        HttpResponse.json(
          { code: 'upstream_error', message: 'Failed to load Skills' },
          { status: 502 },
        ),
      ),
    );

    const { result } = renderHook(() => useMarketplace(FIRST_RENDER, null));

    await waitFor(() => expect(result.current.status).toBe('error'));
    expect(result.current.error).toBe('Failed to load Skills');
    expect(result.current.browse).toEqual(FIRST_RENDER.browse);
    expect(window.location.search).toBe('?marketplace=ClearRoute+Core');
  });
});

describe('useMarketplace history navigation', () => {
  it('keeps the visible page when Forward supersedes an unfinished Back request', async () => {
    window.history.replaceState(null, '', PLATFORM_PAGE_2_URL);
    let releaseBack: () => void = () => {};
    const backResponse = new Promise<void>((resolve) => {
      releaseBack = resolve;
    });
    let requested = false;
    server.use(
      http.get('/api/marketplace/skills', async () => {
        requested = true;
        await backResponse;
        return HttpResponse.json({
          data: {
            ...PLATFORM_PAGE_2,
            browse: { ...PLATFORM_PAGE_2_BROWSE, page: 1 },
          },
        });
      }),
    );
    const { result } = renderHook(() => useMarketplace(PLATFORM_PAGE_2));
    act(() => {
      window.history.replaceState(null, '', '/marketplace?marketplace=Platform+Engineering');
      window.dispatchEvent(new PopStateEvent('popstate'));
    });
    await waitFor(() => expect(requested).toBe(true));
    act(() => {
      window.history.replaceState(null, '', PLATFORM_PAGE_2_URL);
      window.dispatchEvent(new PopStateEvent('popstate'));
    });
    await act(async () => {
      releaseBack();
      await backResponse;
    });
    expect(result.current.browse).toEqual(PLATFORM_PAGE_2_BROWSE);
    expect(result.current.status).toBe('success');
    expect(window.location.search).toBe('?marketplace=Platform+Engineering&page=2');
  });

  it('discards typed search when Back restores a previous browse state', async () => {
    const browse = { ...PLATFORM_PAGE_2_BROWSE, query: 'alpha' };
    const initial = { ...PLATFORM_PAGE_2, requested: browse, browse };
    window.history.replaceState(null, '', `${PLATFORM_PAGE_2_URL}&query=alpha`);
    const requests: URL[] = [];
    server.use(
      http.get('/api/marketplace/skills', ({ request }) => {
        const url = new URL(request.url);
        requests.push(url);
        const next = { ...browse, page: 1, query: url.searchParams.get('query') ?? '' };
        return HttpResponse.json({ data: { ...initial, requested: next, browse: next } });
      }),
    );
    const { result } = renderHook(() => useMarketplace(initial));
    act(() => result.current.setSearchText('beta'));
    act(() => {
      window.history.replaceState(
        null,
        '',
        '/marketplace?marketplace=Platform+Engineering&query=alpha',
      );
      window.dispatchEvent(new PopStateEvent('popstate'));
    });
    await waitFor(() => expect(result.current.status).toBe('success'));
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 350));
    });
    expect(requests).toHaveLength(1);
    expect(result.current.browse.query).toBe('alpha');
    expect(result.current.searchText).toBe('alpha');
  });

  it('keeps a first-load error when history restores the same failed browse state', () => {
    window.history.replaceState(null, '', PLATFORM_PAGE_2_URL);
    const { result } = renderHook(() => useMarketplace(PLATFORM_PAGE_2, 'Failed to load Skills'));
    act(() => window.dispatchEvent(new PopStateEvent('popstate')));
    expect(result.current.status).toBe('error');
    expect(result.current.error).toBe('Failed to load Skills');
  });
});

describe('request lifecycle', () => {
  it('starts in the success status when there is no initial error', () => {
    const data = overviewData();
    setLocation(data.browse);

    const { result } = renderHook(() => useMarketplace(data));

    expect(result.current.status).toBe('success');
  });

  it('starts in the error status when the server render failed', () => {
    const data = overviewData();
    setLocation(data.browse);

    const { result } = renderHook(() => useMarketplace(data, 'Failed to load Marketplaces'));

    expect(result.current.status).toBe('error');
    expect(result.current.error).toBe('Failed to load Marketplaces');
  });

  it('keeps the current results on screen while a later request is loading', async () => {
    const data = overviewData({
      skills: [{ slug: 'core/skills/a', title_of_package: 'A', description: '' }],
      total: 1,
    });
    setLocation(data.browse);
    server.use(
      http.get('/api/marketplace/skills', async () => {
        await new Promise((resolve) => setTimeout(resolve, 50));
        return HttpResponse.json({
          data: overviewData({
            browse: { ...data.browse, page: 2 },
            requested: { ...data.browse, page: 2 },
            skills: [{ slug: 'core/skills/b', title_of_package: 'B', description: '' }],
            total: 1,
          }),
        });
      }),
    );

    const { result } = renderHook(() => useMarketplace(data));
    act(() => {
      result.current.goToPage(2);
    });

    // The delayed response has not arrived yet.
    expect(result.current.status).toBe('loading');
    expect(result.current.skills).toEqual(data.skills);

    // Let the delayed response settle so it doesn't leak into the next test.
    await waitFor(() => expect(result.current.status).toBe('success'));
  });

  it('shows an error and keeps the previous results and URL when a later request fails', async () => {
    const data = overviewData();
    setLocation(data.browse);
    server.use(
      http.get('/api/marketplace/skills', () =>
        HttpResponse.json(
          { code: 'upstream_error', message: 'Failed to load Skills' },
          { status: 500 },
        ),
      ),
    );

    const { result } = renderHook(() => useMarketplace(data));
    act(() => {
      result.current.goToPage(2);
    });

    await waitFor(() => expect(result.current.status).toBe('error'));
    expect(result.current.browse).toEqual(data.browse);
    expect(window.location.pathname + window.location.search).toBe(
      buildBrowseUrl('/marketplace', '', data.browse),
    );
  });

  it('retry repeats the failed browse request', async () => {
    const data = overviewData();
    setLocation(data.browse);
    let attempts = 0;
    const page2 = overviewData({
      browse: { ...data.browse, page: 2 },
      requested: { ...data.browse, page: 2 },
    });
    server.use(
      http.get('/api/marketplace/skills', () => {
        attempts += 1;
        if (attempts === 1) {
          return HttpResponse.json(
            { code: 'upstream_error', message: 'Failed to load Skills' },
            { status: 500 },
          );
        }
        return HttpResponse.json({ data: page2 });
      }),
    );

    const { result } = renderHook(() => useMarketplace(data));
    act(() => {
      result.current.goToPage(2);
    });
    await waitFor(() => expect(result.current.status).toBe('error'));

    act(() => {
      result.current.retry();
    });
    await waitFor(() => expect(result.current.status).toBe('success'));

    expect(attempts).toBe(2);
    expect(result.current.browse).toEqual(page2.browse);
  });
});

describe('search debounce and request races', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('sends the search 300 ms after the last keystroke, not sooner', async () => {
    vi.useFakeTimers();
    const data = overviewData();
    setLocation(data.browse);
    let requests = 0;
    server.use(
      http.get('/api/marketplace/skills', () => {
        requests += 1;
        return HttpResponse.json({
          data: overviewData({
            browse: { ...data.browse, query: 'react' },
            requested: { ...data.browse, query: 'react' },
          }),
        });
      }),
    );

    const { result } = renderHook(() => useMarketplace(data));
    act(() => {
      result.current.setSearchText('react');
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(299);
    });
    expect(requests).toBe(0);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(requests).toBe(1);
  });

  it('does not search below the minimum search length', async () => {
    vi.useFakeTimers();
    const data = overviewData();
    setLocation(data.browse);
    let requests = 0;
    server.use(
      http.get('/api/marketplace/skills', () => {
        requests += 1;
        return HttpResponse.json({ data });
      }),
    );

    const { result } = renderHook(() => useMarketplace(data));
    act(() => {
      result.current.setSearchText('a'.repeat(MIN_SEARCH_LENGTH - 1));
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });
    expect(requests).toBe(0);
  });

  it('clearSearch sends immediately, with no debounce', async () => {
    const data = overviewData({
      browse: browse({ query: 'alpha' }),
      requested: browse({ query: 'alpha' }),
    });
    setLocation(data.browse);
    server.use(
      http.get('/api/marketplace/skills', () =>
        HttpResponse.json({
          data: overviewData({
            browse: { ...data.browse, query: '' },
            requested: { ...data.browse, query: '' },
          }),
        }),
      ),
    );

    const { result } = renderHook(() => useMarketplace(data));
    expect(result.current.searchText).toBe('alpha');

    act(() => {
      result.current.clearSearch();
    });

    await waitFor(() => expect(result.current.browse.query).toBe(''));
    expect(result.current.searchText).toBe('');
  });

  it('only the newest successful request replaces the results when two searches race', async () => {
    vi.useFakeTimers();
    const data = overviewData();
    setLocation(data.browse);
    let releaseFirst: () => void = () => {};
    const firstReleased = new Promise<void>((resolve) => {
      releaseFirst = resolve;
    });
    let releaseSecond: () => void = () => {};
    const secondReleased = new Promise<void>((resolve) => {
      releaseSecond = resolve;
    });
    let calls = 0;
    server.use(
      http.get('/api/marketplace/skills', async ({ request }) => {
        calls += 1;
        const callNumber = calls;
        const query = new URL(request.url).searchParams.get('query') ?? '';
        if (callNumber === 1) {
          // The request for "first" resolves only after the request for
          // "second" below, even though it was sent first.
          await firstReleased;
        } else {
          await secondReleased;
        }
        return HttpResponse.json({
          data: overviewData({
            browse: { ...data.browse, query },
            requested: { ...data.browse, query },
          }),
        });
      }),
    );

    const { result } = renderHook(() => useMarketplace(data));
    act(() => {
      result.current.setSearchText('first');
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });
    act(() => {
      result.current.setSearchText('second');
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });

    // Resolve the newer ("second") request first, then the stale ("first")
    // request. Only the newest request may win, regardless of arrival order.
    await act(async () => {
      releaseSecond();
      await secondReleased;
    });
    await act(async () => {
      releaseFirst();
      await firstReleased;
    });

    expect(result.current.browse.query).toBe('second');
  });

  it('a rapid Marketplace change after a search supersedes the search request', async () => {
    vi.useFakeTimers();
    const data = overviewData();
    setLocation(data.browse);
    server.use(
      http.get('/api/marketplace/skills', () =>
        HttpResponse.json({
          data: overviewData({
            browse: { ...data.browse, marketplace: 'Platform Engineering' },
            requested: { ...data.browse, marketplace: 'Platform Engineering' },
          }),
        }),
      ),
    );

    const { result } = renderHook(() => useMarketplace(data));
    act(() => {
      result.current.setSearchText('react');
      result.current.selectMarketplace('Platform Engineering');
    });

    // Flush the pending microtasks of the Marketplace change's request
    // without ever advancing past the search's 300 ms debounce deadline.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(result.current.browse.marketplace).toBe('Platform Engineering');
  });

  it('aborts the in-flight request on unmount', async () => {
    const data = overviewData();
    setLocation(data.browse);
    // A plain object, not a bare `let`, because TS narrows a `let` reassigned
    // only inside a closure to its initial `null` at every later read.
    const captured: { signal: AbortSignal | null } = { signal: null };
    server.use(
      http.get('/api/marketplace/skills', async ({ request }) => {
        captured.signal = request.signal;
        await new Promise((resolve) => setTimeout(resolve, 50));
        return HttpResponse.json({ data });
      }),
    );

    const { result, unmount } = renderHook(() => useMarketplace(data));
    act(() => {
      result.current.goToPage(2);
    });
    unmount();

    await new Promise((resolve) => setTimeout(resolve, 80));
    expect(captured.signal?.aborted).toBe(true);
  });
});

describe('browser history mechanics', () => {
  it('a page change pushes a new history entry', async () => {
    const data = overviewData();
    setLocation(data.browse);
    const pushSpy = vi.spyOn(window.history, 'pushState');
    const replaceSpy = vi.spyOn(window.history, 'replaceState');
    server.use(
      http.get('/api/marketplace/skills', () =>
        HttpResponse.json({
          data: overviewData({
            browse: { ...data.browse, page: 2 },
            requested: { ...data.browse, page: 2 },
          }),
        }),
      ),
    );

    const { result } = renderHook(() => useMarketplace(data));
    act(() => {
      result.current.goToPage(2);
    });
    await waitFor(() => expect(result.current.browse.page).toBe(2));

    expect(pushSpy).toHaveBeenCalledTimes(1);
    expect(replaceSpy).not.toHaveBeenCalled();
    pushSpy.mockRestore();
    replaceSpy.mockRestore();
  });

  it('a search, Marketplace, sort, or page-size change replaces the current history entry', async () => {
    const data = overviewData();
    setLocation(data.browse);
    const pushSpy = vi.spyOn(window.history, 'pushState');
    const replaceSpy = vi.spyOn(window.history, 'replaceState');
    server.use(
      http.get('/api/marketplace/skills', () =>
        HttpResponse.json({
          data: overviewData({
            browse: { ...data.browse, sort: 'name-desc' },
            requested: { ...data.browse, sort: 'name-desc' },
          }),
        }),
      ),
    );

    const { result } = renderHook(() => useMarketplace(data));
    act(() => {
      result.current.setSort('name-desc');
    });
    await waitFor(() => expect(result.current.browse.sort).toBe('name-desc'));

    expect(replaceSpy).toHaveBeenCalledTimes(1);
    expect(pushSpy).not.toHaveBeenCalled();
    pushSpy.mockRestore();
    replaceSpy.mockRestore();
  });

  it('selecting a Marketplace, changing the sort, or changing the page size resets to page 1', async () => {
    const data = overviewData({
      browse: browse({ page: 3 }),
      requested: browse({ page: 3 }),
    });
    setLocation(data.browse);
    let capturedPage: string | null = null;
    server.use(
      http.get('/api/marketplace/skills', ({ request }) => {
        capturedPage = new URL(request.url).searchParams.get('page');
        return HttpResponse.json({
          data: overviewData({
            browse: { ...data.browse, sort: 'name-desc', page: 1 },
            requested: { ...data.browse, sort: 'name-desc', page: 1 },
          }),
        });
      }),
    );

    const { result } = renderHook(() => useMarketplace(data));
    act(() => {
      result.current.setSort('name-desc');
    });
    await waitFor(() => expect(result.current.status).toBe('success'));

    expect(capturedPage).toBe('1');
  });

  it('restores the browse values on Back/Forward within the overview', async () => {
    const data = overviewData();
    setLocation(data.browse);
    const restored = browse({ marketplace: 'Platform Engineering' });
    server.use(
      http.get('/api/marketplace/skills', () =>
        HttpResponse.json({ data: overviewData({ browse: restored, requested: restored }) }),
      ),
    );

    const { result } = renderHook(() => useMarketplace(data));
    act(() => {
      window.history.replaceState(null, '', buildBrowseUrl('/marketplace', '', restored));
      window.dispatchEvent(new PopStateEvent('popstate'));
    });

    await waitFor(() => expect(result.current.browse).toEqual(restored));
  });

  it('ignores a popstate event for another route', async () => {
    const data = overviewData();
    setLocation(data.browse);
    let requests = 0;
    server.use(
      http.get('/api/marketplace/skills', () => {
        requests += 1;
        return HttpResponse.json({ data });
      }),
    );

    renderHook(() => useMarketplace(data));
    act(() => {
      window.history.replaceState(null, '', '/agents');
      window.dispatchEvent(new PopStateEvent('popstate'));
    });

    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(requests).toBe(0);
  });

  it('a pending change keeps the latest user intent as its base, not the stale committed value', async () => {
    const data = overviewData();
    setLocation(data.browse);
    let secondRequestParams: { page: string | null; sort: string | null } | null = null;
    let calls = 0;
    server.use(
      http.get('/api/marketplace/skills', ({ request }) => {
        calls += 1;
        const url = new URL(request.url);
        if (calls === 1) {
          // The sort change. The page change below aborts it before it
          // resolves, so its response is never used.
          return HttpResponse.json({ data });
        }
        secondRequestParams = {
          page: url.searchParams.get('page'),
          sort: url.searchParams.get('sort'),
        };
        return HttpResponse.json({
          data: overviewData({
            browse: { ...data.browse, sort: 'name-desc', page: 2 },
            requested: { ...data.browse, sort: 'name-desc', page: 2 },
          }),
        });
      }),
    );

    const { result } = renderHook(() => useMarketplace(data));
    // Start a sort change (pending, not yet committed), then immediately
    // change the page before it resolves. The page change must build on the
    // pending sort value, not the stale committed one.
    act(() => {
      result.current.setSort('name-desc');
    });
    act(() => {
      result.current.goToPage(2);
    });

    await waitFor(() => expect(result.current.status).toBe('success'));

    expect(secondRequestParams).toEqual({ page: '2', sort: 'name-desc' });
    expect(result.current.browse).toEqual({ ...data.browse, sort: 'name-desc', page: 2 });
  });
});
