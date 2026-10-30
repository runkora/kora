/**
 * Integration tests for useMarketplace's state and BFF requests.
 *
 * The selected Marketplace is reflected in the overview path. Other browse
 * values stay in React state and cannot create pagination history.
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
import { parseBrowseParams } from '../_utils/browse-params';
import { useMarketplace } from './use-marketplace';

vi.mock('next/navigation', () => ({ usePathname: () => window.location.pathname }));

// ── Fixtures ──────────────────────────────────────────────────────────────────

const MARKETPLACES = [
  {
    name: 'ClearRoute Core',
    marketplaceSlug: 'ClearRoute Core',
    link: 'https://github.com/example/clearroute-core',
  },
  {
    name: 'Platform Engineering',
    marketplaceSlug: 'Platform Engineering',
    link: 'https://github.com/example/platform-engineering',
  },
];

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
  skills: [{ skillSlug: 'core/skills/first', skillName: 'first', name: 'First', description: '' }],
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
    {
      skillSlug: 'platform/skills/twenty-five',
      skillName: 'twenty-five',
      name: 'Twenty-five',
      description: '',
    },
  ],
  total: 30,
};

/** An old overview link whose browse values are no longer restored. */
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

/** Starts every browse action from the clean overview address. */
function setLocation() {
  window.history.replaceState(null, '', '/marketplace');
}

afterEach(() => {
  window.history.replaceState(null, '', '/');
});

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('useMarketplace initial state', () => {
  it('removes old browse parameters without overriding server data or refetching', () => {
    window.history.replaceState(null, '', `${PLATFORM_PAGE_2_URL}#results`);
    const historyLength = window.history.length;
    const requests: URL[] = [];
    server.use(
      http.get('/api/marketplace/skills', ({ request }) => {
        requests.push(new URL(request.url));
        return HttpResponse.json({ data: PLATFORM_PAGE_2 });
      }),
    );

    const { result } = renderHook(() => useMarketplace(FIRST_RENDER, null));

    expect(result.current.browse).toEqual(FIRST_RENDER.browse);
    expect(result.current.status).toBe('success');
    expect(result.current.skills).toEqual(FIRST_RENDER.skills);
    expect(requests).toHaveLength(0);
    expect(window.location.pathname + window.location.search).toBe(
      '/marketplace/ClearRoute%20Core',
    );
    expect(window.location.hash).toBe('#results');
    expect(window.history.length).toBe(historyLength);
  });

  it('puts the server-selected Marketplace in the overview path without refetching', () => {
    setLocation();
    const savedMarketplace = { ...PLATFORM_PAGE_2_BROWSE, page: 1 };
    const initial = { ...PLATFORM_PAGE_2, requested: savedMarketplace, browse: savedMarketplace };
    const requests: URL[] = [];
    server.use(
      http.get('/api/marketplace/skills', ({ request }) => {
        requests.push(new URL(request.url));
        return HttpResponse.json({ data: FIRST_RENDER });
      }),
    );

    const { result } = renderHook(() => useMarketplace(initial, null));

    expect(requests).toHaveLength(0);
    expect(result.current.status).toBe('success');
    expect(result.current.browse).toEqual(savedMarketplace);
    expect(window.location.pathname + window.location.search).toBe(
      '/marketplace/Platform%20Engineering',
    );
  });

  it('preserves unrelated query parameters and the fragment when setting the path', () => {
    window.history.replaceState(null, '', '/marketplace?agent=123&page=3#results');
    renderHook(() => useMarketplace(FIRST_RENDER));
    expect(window.location.pathname + window.location.search + window.location.hash).toBe(
      '/marketplace/ClearRoute%20Core?agent=123#results',
    );
  });

  it('keeps the Storybook preview path when rendering its Marketplace fixture', () => {
    window.history.replaceState(null, '', '/iframe.html?id=pages-marketplace--default');
    renderHook(() => useMarketplace(FIRST_RENDER));
    expect(window.location.pathname + window.location.search).toBe(
      '/iframe.html?id=pages-marketplace--default',
    );
  });

  it('does not rewrite a Skill detail address while the overview is still mounted', () => {
    const path = '/marketplace/team__repo/reviewer';
    window.history.replaceState(null, '', path);
    renderHook(() => useMarketplace(FIRST_RENDER));
    expect(window.location.pathname).toBe(path);
  });

  it('reloads the path selection when Back restores a cached overview from another Marketplace', async () => {
    const path = '/marketplace/Platform%20Engineering';
    window.history.replaceState(null, '', path);
    const requests: URL[] = [];
    const restoredBrowse = { ...PLATFORM_PAGE_2_BROWSE, page: 1 };
    server.use(
      http.get('/api/marketplace/skills', ({ request }) => {
        requests.push(new URL(request.url));
        return HttpResponse.json({
          data: { ...PLATFORM_PAGE_2, requested: restoredBrowse, browse: restoredBrowse },
        });
      }),
    );

    const { result } = renderHook(() => useMarketplace(FIRST_RENDER));
    expect(window.location.pathname).toBe(path);
    await waitFor(() => expect(result.current.browse.marketplace).toBe('Platform Engineering'));
    expect(requests).toHaveLength(1);
    expect(requests[0].searchParams.get('marketplace')).toBe('Platform Engineering');
    expect(window.location.pathname).toBe(path);
  });

  it('keeps the server fallback for an unknown path selection without refetching', () => {
    window.history.replaceState(null, '', '/marketplace/missing');
    const initial = { ...FIRST_RENDER, requested: { ...DEFAULT_BROWSE, marketplace: 'missing' } };
    const requests: URL[] = [];
    server.use(
      http.get('/api/marketplace/skills', ({ request }) => {
        requests.push(new URL(request.url));
        return HttpResponse.json({ data: FIRST_RENDER });
      }),
    );
    renderHook(() => useMarketplace(initial));
    expect(requests).toHaveLength(0);
    expect(window.location.pathname).toBe('/marketplace/ClearRoute%20Core');
  });

  it('keeps an initial error retryable using server browse values and a clean address', async () => {
    window.history.replaceState(null, '', PLATFORM_PAGE_2_URL);
    const requests: URL[] = [];
    server.use(
      http.get('/api/marketplace/skills', ({ request }) => {
        requests.push(new URL(request.url));
        return HttpResponse.json({ data: FIRST_RENDER });
      }),
    );

    const { result } = renderHook(() => useMarketplace(FIRST_RENDER, 'Failed to load Skills'));

    expect(result.current.status).toBe('error');
    expect(result.current.error).toBe('Failed to load Skills');
    expect(window.location.pathname + window.location.search).toBe(
      '/marketplace/ClearRoute%20Core',
    );
    expect(requests).toHaveLength(0);

    act(() => result.current.retry());
    await waitFor(() => expect(result.current.status).toBe('success'));

    expect(requests).toHaveLength(1);
    expect(requests[0].searchParams.get('marketplace')).toBe('ClearRoute Core');
    expect(requests[0].searchParams.get('page')).toBe('1');
    expect(result.current.browse).toEqual(FIRST_RENDER.browse);
    expect(window.location.pathname + window.location.search).toBe(
      '/marketplace/ClearRoute%20Core',
    );
  });

  it('starts a fresh mount from server defaults instead of remembering the previous page', async () => {
    setLocation();
    server.use(
      http.get('/api/marketplace/skills', () => HttpResponse.json({ data: PLATFORM_PAGE_2 })),
    );
    const firstMount = renderHook(() => useMarketplace(FIRST_RENDER));
    act(() => firstMount.result.current.goToPage(2));
    await waitFor(() => expect(firstMount.result.current.browse.page).toBe(2));
    firstMount.unmount();
    setLocation();

    const freshMount = renderHook(() => useMarketplace(FIRST_RENDER));

    expect(freshMount.result.current.browse).toEqual(FIRST_RENDER.browse);
    expect(freshMount.result.current.searchText).toBe('');
    expect(window.location.pathname + window.location.search).toBe(
      '/marketplace/ClearRoute%20Core',
    );
  });
});

describe('request lifecycle', () => {
  it('starts in the success status when there is no initial error', () => {
    const data = overviewData();
    setLocation();

    const { result } = renderHook(() => useMarketplace(data));

    expect(result.current.status).toBe('success');
  });

  it('starts in the error status when the server render failed', () => {
    const data = overviewData();
    setLocation();

    const { result } = renderHook(() => useMarketplace(data, 'Failed to load Marketplaces'));

    expect(result.current.status).toBe('error');
    expect(result.current.error).toBe('Failed to load Marketplaces');
  });

  it('keeps the current results on screen while a later request is loading', async () => {
    const data = overviewData({
      skills: [{ skillSlug: 'core/skills/a', skillName: 'a', name: 'A', description: '' }],
      total: 1,
    });
    setLocation();
    server.use(
      http.get('/api/marketplace/skills', async () => {
        await new Promise((resolve) => setTimeout(resolve, 50));
        return HttpResponse.json({
          data: overviewData({
            browse: { ...data.browse, page: 2 },
            requested: { ...data.browse, page: 2 },
            skills: [{ skillSlug: 'core/skills/b', skillName: 'b', name: 'B', description: '' }],
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
    setLocation();
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
      result.current.selectMarketplace('Platform Engineering');
    });

    await waitFor(() => expect(result.current.status).toBe('error'));
    expect(result.current.browse).toEqual(data.browse);
    expect(window.location.pathname + window.location.search).toBe(
      '/marketplace/ClearRoute%20Core',
    );
  });

  it('retry repeats the failed browse request', async () => {
    const data = overviewData();
    setLocation();
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
  it('starts a search in relevance order and restores the previous browse sort on clear', async () => {
    const initialBrowse = browse({ sort: 'name-desc', page: 3 });
    const data = overviewData({ browse: initialBrowse, requested: initialBrowse });
    setLocation();
    const requests: MarketplaceBrowseParams[] = [];
    server.use(
      http.get('/api/marketplace/skills', ({ request }) => {
        const next = parseBrowseParams(new URL(request.url).searchParams);
        requests.push(next);
        return HttpResponse.json({ data: overviewData({ browse: next, requested: next }) });
      }),
    );
    const { result } = renderHook(() => useMarketplace(data));
    act(() => result.current.setSearchText('validation'));
    await waitFor(() => expect(result.current.browse.query).toBe('validation'));
    expect(result.current.browse.sort).toBe('relevance');
    expect(result.current.browse.page).toBe(1);
    act(() => result.current.clearSearch());
    await waitFor(() => expect(result.current.browse.query).toBe(''));
    expect(result.current.browse.sort).toBe('name-desc');
    expect(requests.map(({ sort }) => sort)).toEqual(['relevance', 'name-desc']);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('sends the search 300 ms after the last keystroke, not sooner', async () => {
    vi.useFakeTimers();
    const data = overviewData();
    setLocation();
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
    setLocation();
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
    setLocation();
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
    setLocation();
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
    setLocation();
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
    setLocation();
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

describe('browse actions with a Marketplace path', () => {
  it('a page change sends all browse values to the API without changing address or history', async () => {
    const currentBrowse = browse({ query: 'react', sort: 'name-desc' });
    const data = overviewData({ requested: currentBrowse, browse: currentBrowse });
    setLocation();
    const requests: URL[] = [];
    server.use(
      http.get('/api/marketplace/skills', ({ request }) => {
        requests.push(new URL(request.url));
        return HttpResponse.json({
          data: overviewData({
            browse: { ...data.browse, page: 2 },
            requested: { ...data.browse, page: 2 },
          }),
        });
      }),
    );

    const { result } = renderHook(() => useMarketplace(data));
    const pushSpy = vi.spyOn(window.history, 'pushState');
    const replaceSpy = vi.spyOn(window.history, 'replaceState');
    const historyLength = window.history.length;
    act(() => {
      result.current.goToPage(2);
    });
    await waitFor(() => expect(result.current.browse.page).toBe(2));

    expect(requests).toHaveLength(1);
    expect(Object.fromEntries(requests[0].searchParams)).toEqual({
      marketplace: 'ClearRoute Core',
      query: 'react',
      page: '2',
      pageSize: '24',
      sort: 'name-desc',
    });
    expect(window.location.pathname + window.location.search).toBe(
      '/marketplace/ClearRoute%20Core',
    );
    expect(window.history.length).toBe(historyLength);
    expect(pushSpy).not.toHaveBeenCalled();
    expect(replaceSpy).not.toHaveBeenCalled();
    pushSpy.mockRestore();
    replaceSpy.mockRestore();
  });

  it('updates the Marketplace path once while keeping other browse actions out of the URL', async () => {
    const data = overviewData();
    setLocation();
    const requests: URL[] = [];
    server.use(
      http.get('/api/marketplace/skills', ({ request }) => {
        const url = new URL(request.url);
        requests.push(url);
        const next = parseBrowseParams(url.searchParams);
        return HttpResponse.json({ data: overviewData({ browse: next, requested: next }) });
      }),
    );

    const { result } = renderHook(() => useMarketplace(data));
    const pushSpy = vi.spyOn(window.history, 'pushState');
    const replaceSpy = vi.spyOn(window.history, 'replaceState');
    act(() => result.current.selectMarketplace('Platform Engineering'));
    await waitFor(() => expect(result.current.browse.marketplace).toBe('Platform Engineering'));
    act(() => result.current.setSort('name-desc'));
    await waitFor(() => expect(result.current.browse.sort).toBe('name-desc'));
    act(() => result.current.setSearchText('react'));
    await waitFor(() => expect(result.current.browse.query).toBe('react'));

    expect(requests).toHaveLength(3);
    expect(Object.fromEntries(requests[2].searchParams)).toEqual({
      marketplace: 'Platform Engineering',
      query: 'react',
      page: '1',
      pageSize: '24',
      sort: 'relevance',
    });
    act(() => result.current.clearSearch());
    await waitFor(() => expect(result.current.browse.query).toBe(''));
    expect(requests).toHaveLength(4);
    expect(requests[3].searchParams.has('query')).toBe(false);
    expect(window.location.pathname + window.location.search).toBe(
      '/marketplace/Platform%20Engineering',
    );
    expect(replaceSpy).toHaveBeenCalledTimes(1);
    expect(pushSpy).not.toHaveBeenCalled();
    pushSpy.mockRestore();
    replaceSpy.mockRestore();
  });

  it.each(['Marketplace', 'sort'])('changing %s resets to page 1', async (change) => {
    const data = overviewData({
      browse: browse({ page: 3 }),
      requested: browse({ page: 3 }),
    });
    setLocation();
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
      if (change === 'Marketplace') {
        result.current.selectMarketplace('Platform Engineering');
      } else {
        result.current.setSort('name-desc');
      }
    });
    await waitFor(() => expect(result.current.status).toBe('success'));

    expect(capturedPage).toBe('1');
  });

  it('a pending change keeps the latest user intent as its base, not the stale committed value', async () => {
    const data = overviewData();
    setLocation();
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
