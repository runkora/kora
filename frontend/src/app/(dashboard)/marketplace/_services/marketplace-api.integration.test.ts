/**
 * Integration tests for the Marketplace browser fetch helper.
 *
 * MSW intercepts the BFF route; no real Next.js server runs. MSW ignores the
 * query string when matching a handler pattern, so one pattern matches every
 * browse value this file sends.
 */
import { describe, it, expect, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { ApiRoute } from '@orbit/lib/routing';
import { server } from '@orbit/test-utils';
import { fetchMarketplaceOverview } from './marketplace-api';
import type { MarketplaceBrowseParams, MarketplaceOverviewData } from '../_types';

const BROWSE: MarketplaceBrowseParams = {
  marketplace: 'Core',
  query: '',
  page: 1,
  pageSize: 24,
  sort: 'name-asc',
};

const SKILLS_URL = ApiRoute.marketplaceSkills.GET(BROWSE);

const OVERVIEW_FIXTURE: MarketplaceOverviewData = {
  marketplaces: [{ name: 'Core' }],
  requested: BROWSE,
  browse: BROWSE,
  skills: [],
  total: 0,
};

describe('fetchMarketplaceOverview', () => {
  it('returns the overview data on a successful response', async () => {
    server.use(http.get(SKILLS_URL, () => HttpResponse.json({ data: OVERVIEW_FIXTURE })));

    const result = await fetchMarketplaceOverview(BROWSE, new AbortController().signal);

    expect(result).toEqual({ overview: OVERVIEW_FIXTURE, error: null, cancelled: false });
  });

  it('reads the error message from a failed response body', async () => {
    server.use(
      http.get(SKILLS_URL, () =>
        HttpResponse.json(
          { code: 'upstream_error', message: 'Failed to load Skills' },
          { status: 502 },
        ),
      ),
    );

    const result = await fetchMarketplaceOverview(BROWSE, new AbortController().signal);

    expect(result.overview).toBeNull();
    expect(result.cancelled).toBe(false);
    expect(result.error).toBe('Failed to load Skills');
  });

  it('falls back to the status code when a failed response has no message', async () => {
    server.use(http.get(SKILLS_URL, () => HttpResponse.json({}, { status: 403 })));

    const result = await fetchMarketplaceOverview(BROWSE, new AbortController().signal);

    expect(result.error).toBe('Request failed (403)');
  });

  it('falls back to the status code when a failed response body is not JSON', async () => {
    server.use(http.get(SKILLS_URL, () => new HttpResponse('not json', { status: 500 })));

    const result = await fetchMarketplaceOverview(BROWSE, new AbortController().signal);

    expect(result.error).toBe('Request failed (500)');
  });

  it('reports a network error when the request cannot reach the server', async () => {
    server.use(http.get(SKILLS_URL, () => HttpResponse.error()));

    const result = await fetchMarketplaceOverview(BROWSE, new AbortController().signal);

    expect(result.overview).toBeNull();
    expect(result.cancelled).toBe(false);
    expect(result.error).toBe('Network error: could not reach the server.');
  });

  it('reports an invalid-response error when a successful body is not valid JSON', async () => {
    server.use(http.get(SKILLS_URL, () => new HttpResponse('not json', { status: 200 })));

    const result = await fetchMarketplaceOverview(BROWSE, new AbortController().signal);

    expect(result.overview).toBeNull();
    expect(result.error).toBe('Invalid response from the server.');
  });

  it('reports a cancellation with no user-facing error when the signal aborts before the response', async () => {
    const controller = new AbortController();
    const originalFetch = global.fetch;

    // Mock fetch to throw AbortError when signal is aborted
    const fetchSpy = vi
      .spyOn(global, 'fetch')
      .mockImplementation(async (url: RequestInfo | URL, opts?: RequestInit) => {
        const signal = opts?.signal;

        if (signal) {
          // Check if signal is already aborted
          if (signal.aborted) {
            throw new DOMException('The operation was aborted.', 'AbortError');
          }

          // Monitor the signal and throw if it gets aborted while waiting
          return new Promise((resolve, reject) => {
            const abortHandler = () => {
              reject(new DOMException('The operation was aborted.', 'AbortError'));
            };
            signal.addEventListener('abort', abortHandler);

            // Simulate slow response
            setTimeout(() => {
              signal.removeEventListener('abort', abortHandler);
              if (!signal.aborted) {
                resolve(new Response(JSON.stringify({ data: OVERVIEW_FIXTURE }), { status: 200 }));
              }
            }, 100);
          });
        }

        return originalFetch(url, opts);
      });

    const promise = fetchMarketplaceOverview(BROWSE, controller.signal);

    // Abort after a short delay
    setTimeout(() => {
      controller.abort();
    }, 10);

    const result = await promise;

    fetchSpy.mockRestore();

    expect(result).toEqual({ overview: null, error: null, cancelled: true });
  });

  it('reports a cancellation with no user-facing error when the signal aborts while reading the body', async () => {
    const abortError = new DOMException('The operation was aborted.', 'AbortError');
    const fakeResponse = { ok: true, json: () => Promise.reject(abortError) } as Response;
    const fetchSpy = vi.spyOn(global, 'fetch').mockResolvedValue(fakeResponse);

    const result = await fetchMarketplaceOverview(BROWSE, new AbortController().signal);

    expect(result).toEqual({ overview: null, error: null, cancelled: true });
    fetchSpy.mockRestore();
  });
});
