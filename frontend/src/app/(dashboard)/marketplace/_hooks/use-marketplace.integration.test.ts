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
import { afterEach, describe, expect, it } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { server } from '@orbit/test-utils';
import type { MarketplaceBrowseParams, MarketplaceOverviewData } from '../_types';
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
