/**
 * Unit tests for clean navigation URLs and API browse parsing.
 */
import { describe, expect, it } from 'vitest';
import {
  PAGE_SIZE,
  DEFAULT_SORT,
  MIN_SEARCH_LENGTH,
  type MarketplaceBrowseParams,
} from '../_types';
import {
  cleanBrowseUrl,
  parseBrowseParams,
  recordToSearchParams,
  toResultState,
  toSearchQuery,
} from './browse-params';

// ── Fixtures ──────────────────────────────────────────────────────────────────

/** Page 3 of a search in "Platform Engineering", with other values that are not defaults. */
const PAGE_3_BROWSE: MarketplaceBrowseParams = {
  marketplace: 'Platform Engineering',
  query: 'review',
  page: 3,
  pageSize: 24,
  sort: 'name-desc',
};

const PAGE_3_QUERY =
  'marketplace=Platform+Engineering&query=review&page=3&pageSize=12&sort=name-desc';

// ── Tests ─────────────────────────────────────────────────────────────────────

function params(query: string): URLSearchParams {
  return new URLSearchParams(query);
}

describe('parseBrowseParams', () => {
  it('keeps the complete browse values available to API requests', () => {
    expect(parseBrowseParams(params(PAGE_3_QUERY))).toEqual(PAGE_3_BROWSE);
  });

  it('returns all defaults for an empty query', () => {
    expect(parseBrowseParams(params(''))).toEqual<MarketplaceBrowseParams>({
      marketplace: '',
      query: '',
      page: 1,
      pageSize: PAGE_SIZE,
      sort: DEFAULT_SORT,
    });
  });

  it('reads a Marketplace name from the URL', () => {
    expect(parseBrowseParams(params('marketplace=ClearRoute+Core')).marketplace).toBe(
      'ClearRoute Core',
    );
  });

  it('treats an empty marketplace value as no Marketplace', () => {
    expect(parseBrowseParams(params('marketplace=')).marketplace).toBe('');
  });

  describe('page', () => {
    it('defaults to 1 when the URL has no page', () => {
      expect(parseBrowseParams(params('')).page).toBe(1);
    });

    it('defaults to 1 for a non-numeric value', () => {
      expect(parseBrowseParams(params('page=abc')).page).toBe(1);
    });

    it('defaults to 1 for zero', () => {
      expect(parseBrowseParams(params('page=0')).page).toBe(1);
    });

    it('defaults to 1 for a negative value', () => {
      expect(parseBrowseParams(params('page=-5')).page).toBe(1);
    });

    it('defaults to 1 for a decimal value', () => {
      expect(parseBrowseParams(params('page=1.5')).page).toBe(1);
    });

    it('accepts a valid whole page number', () => {
      expect(parseBrowseParams(params('page=3')).page).toBe(3);
    });
  });

  describe('pageSize', () => {
    it('defaults when the URL has no pageSize', () => {
      expect(parseBrowseParams(params('')).pageSize).toBe(PAGE_SIZE);
    });

    it('defaults for a size that is not one of the options', () => {
      expect(parseBrowseParams(params('pageSize=10')).pageSize).toBe(PAGE_SIZE);
    });

    it('defaults for a non-numeric value', () => {
      expect(parseBrowseParams(params('pageSize=abc')).pageSize).toBe(PAGE_SIZE);
    });

    it('keeps 24 rows when an older link requests 12', () => {
      expect(parseBrowseParams(params('pageSize=12')).pageSize).toBe(24);
    });

    it('keeps 24 rows when an older link requests 48', () => {
      expect(parseBrowseParams(params('pageSize=48')).pageSize).toBe(24);
    });
  });

  describe('sort', () => {
    it('defaults an active search to relevance', () => {
      expect(parseBrowseParams(params('query=validation')).sort).toBe('relevance');
    });

    it('accepts explicit alphabetical sorting during search', () => {
      expect(parseBrowseParams(params('query=validation&sort=name-desc')).sort).toBe('name-desc');
    });

    it('uses normal browsing order when relevance has no valid search', () => {
      expect(parseBrowseParams(params('query=ab&sort=relevance')).sort).toBe(DEFAULT_SORT);
    });

    it('defaults when the URL has no sort', () => {
      expect(parseBrowseParams(params('')).sort).toBe(DEFAULT_SORT);
    });

    it('defaults for an unknown value', () => {
      expect(parseBrowseParams(params('sort=bogus')).sort).toBe(DEFAULT_SORT);
    });

    it('accepts name-desc', () => {
      expect(parseBrowseParams(params('sort=name-desc')).sort).toBe('name-desc');
    });
  });

  describe('repeated parameters', () => {
    it('uses the first value when page repeats', () => {
      expect(parseBrowseParams(params('page=2&page=5')).page).toBe(2);
    });

    it('uses the first value when query repeats', () => {
      expect(parseBrowseParams(params('query=foo&query=bar')).query).toBe('foo');
    });

    it('uses the first value when marketplace repeats', () => {
      expect(parseBrowseParams(params('marketplace=A&marketplace=B')).marketplace).toBe('A');
    });
  });
});

describe('toSearchQuery', () => {
  it('trims leading and trailing whitespace', () => {
    expect(toSearchQuery('  abc  ')).toBe('abc');
  });

  it('returns no search for null', () => {
    expect(toSearchQuery(null)).toBe('');
  });

  it('returns no search for text shorter than the minimum', () => {
    expect(toSearchQuery('a'.repeat(MIN_SEARCH_LENGTH - 1))).toBe('');
  });

  it('keeps text exactly at the minimum length', () => {
    const text = 'a'.repeat(MIN_SEARCH_LENGTH);
    expect(toSearchQuery(text)).toBe(text);
  });

  it('counts only the trimmed length against the minimum', () => {
    expect(toSearchQuery('  ab  ')).toBe('');
  });
});

describe('recordToSearchParams', () => {
  it('sets a key from a single string value', () => {
    expect(recordToSearchParams({ page: '2' }).get('page')).toBe('2');
  });

  it('uses the first value when a key has an array of values', () => {
    expect(recordToSearchParams({ page: ['2', '5'] }).get('page')).toBe('2');
  });

  it('omits a key with an empty array', () => {
    expect(recordToSearchParams({ page: [] }).has('page')).toBe(false);
  });

  it('omits a key with an undefined value', () => {
    expect(recordToSearchParams({ page: undefined }).has('page')).toBe(false);
  });
});

describe('cleanBrowseUrl', () => {
  it('keeps a pathname with no query parameters', () => {
    expect(cleanBrowseUrl('/marketplace', '')).toBe('/marketplace');
  });

  it('removes every browse key, including repeated values', () => {
    expect(cleanBrowseUrl('/marketplace', `?${PAGE_3_QUERY}&page=4`)).toBe('/marketplace');
  });

  it('preserves unrelated query parameters used by the agent builder and Storybook', () => {
    expect(
      cleanBrowseUrl('/iframe.html', `?agent=123&id=app-marketplace--default&${PAGE_3_QUERY}`),
    ).toBe('/iframe.html?agent=123&id=app-marketplace--default');
  });

  it('returns a relative URL that uses the current environment hostname', () => {
    const path = cleanBrowseUrl('/marketplace', '?page=2');
    expect(new URL(path, 'https://kora.example').href).toBe('https://kora.example/marketplace');
  });
});

describe('toResultState', () => {
  it('is populated when there are results, regardless of search', () => {
    expect(toResultState(5, '')).toBe('populated');
    expect(toResultState(5, 'react')).toBe('populated');
  });

  it('is configured_but_empty when there are no results and no search', () => {
    expect(toResultState(0, '')).toBe('configured_but_empty');
  });

  it('is no_matches when there are no results and a search is active', () => {
    expect(toResultState(0, 'react')).toBe('no_matches');
  });
});
