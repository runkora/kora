/**
 * Unit tests for the Skill URL and the back link URL.
 *
 * A Skill URL carries the browse values of the overview. The Skill page reads
 * them back and builds its back link, so that the link returns to the same
 * results (PRD FR-17).
 */
import { describe, expect, it } from 'vitest';
import {
  DEFAULT_PAGE_SIZE,
  DEFAULT_SORT,
  MIN_SEARCH_LENGTH,
  type MarketplaceBrowseParams,
} from '../_types';
import {
  buildBrowseUrl,
  buildOverviewUrl,
  buildSkillUrl,
  parseBrowseParams,
  recordToSearchParams,
  toResultState,
  toSearchQuery,
} from './browse-params';

// ── Fixtures ──────────────────────────────────────────────────────────────────

/** The browse values of `/marketplace` with no query keys. */
const DEFAULT_BROWSE: MarketplaceBrowseParams = {
  marketplace: '',
  query: '',
  page: 1,
  pageSize: 24,
  sort: 'name-asc',
};

/** Page 3 of a search in "Platform Engineering", with other values that are not defaults. */
const PAGE_3_BROWSE: MarketplaceBrowseParams = {
  marketplace: 'Platform Engineering',
  query: 'review',
  page: 3,
  pageSize: 12,
  sort: 'name-desc',
};

const PAGE_3_QUERY =
  'marketplace=Platform+Engineering&query=review&page=3&pageSize=12&sort=name-desc';

const SLUG = 'platform-engineering/skills/react-reviewer';

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('buildSkillUrl', () => {
  it('carries the browse values in the Skill URL', () => {
    expect(buildSkillUrl(SLUG, PAGE_3_BROWSE)).toBe(
      `/marketplace/${encodeURIComponent(SLUG)}?${PAGE_3_QUERY}`,
    );
  });

  it('adds no query for the default values', () => {
    expect(buildSkillUrl(SLUG, DEFAULT_BROWSE)).toBe(`/marketplace/${encodeURIComponent(SLUG)}`);
  });

  it('preserves reserved characters in the slug without changing browse parameters', () => {
    const slug = 'org/repo/a?b#c%20';
    const url = new URL(buildSkillUrl(slug, PAGE_3_BROWSE), 'http://localhost');
    expect(decodeURIComponent(url.pathname.slice('/marketplace/'.length))).toBe(slug);
    expect(url.hash).toBe('');
    expect(parseBrowseParams(url.searchParams)).toEqual(PAGE_3_BROWSE);
  });
});

describe('buildOverviewUrl', () => {
  it('returns to the browse values that the Skill URL carries', () => {
    const skillUrl = new URL(buildSkillUrl(SLUG, PAGE_3_BROWSE), 'http://localhost');
    const browse = parseBrowseParams(skillUrl.searchParams);

    expect(browse).toEqual(PAGE_3_BROWSE);
    expect(buildOverviewUrl(browse)).toBe(`/marketplace?${PAGE_3_QUERY}`);
  });

  it('keeps only the browse values of a Skill URL', () => {
    const browse = parseBrowseParams(
      new URLSearchParams('page=2&agent=new&next=https://example.com'),
    );

    expect(buildOverviewUrl(browse)).toBe('/marketplace?page=2');
  });

  it('opens the plain overview when the Skill URL has no browse values', () => {
    expect(buildOverviewUrl(parseBrowseParams(new URLSearchParams()))).toBe('/marketplace');
  });
});

function params(query: string): URLSearchParams {
  return new URLSearchParams(query);
}

describe('parseBrowseParams', () => {
  it('returns all defaults for an empty query', () => {
    expect(parseBrowseParams(params(''))).toEqual<MarketplaceBrowseParams>({
      marketplace: '',
      query: '',
      page: 1,
      pageSize: DEFAULT_PAGE_SIZE,
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
      expect(parseBrowseParams(params('')).pageSize).toBe(DEFAULT_PAGE_SIZE);
    });

    it('defaults for a size that is not one of the options', () => {
      expect(parseBrowseParams(params('pageSize=10')).pageSize).toBe(DEFAULT_PAGE_SIZE);
    });

    it('defaults for a non-numeric value', () => {
      expect(parseBrowseParams(params('pageSize=abc')).pageSize).toBe(DEFAULT_PAGE_SIZE);
    });

    it('accepts the smallest allowed size', () => {
      expect(parseBrowseParams(params('pageSize=12')).pageSize).toBe(12);
    });

    it('accepts the largest allowed size', () => {
      expect(parseBrowseParams(params('pageSize=48')).pageSize).toBe(48);
    });
  });

  describe('sort', () => {
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

describe('buildBrowseUrl', () => {
  const defaults: MarketplaceBrowseParams = {
    marketplace: '',
    query: '',
    page: 1,
    pageSize: DEFAULT_PAGE_SIZE,
    sort: DEFAULT_SORT,
  };

  it('returns the bare pathname when every browse value is a default and no other keys exist', () => {
    expect(buildBrowseUrl('/marketplace', '', defaults)).toBe('/marketplace');
  });

  it('preserves an unrelated key while rewriting the browse keys', () => {
    const url = buildBrowseUrl('/marketplace', '?agent=123&page=9', { ...defaults, page: 2 });
    const search = new URL(url, 'http://localhost').searchParams;
    expect(search.get('agent')).toBe('123');
    expect(search.get('page')).toBe('2');
  });

  it('omits each browse key that is at its default value', () => {
    const url = buildBrowseUrl('/marketplace', '', { ...defaults, marketplace: 'Core' });
    const search = new URL(url, 'http://localhost').searchParams;
    expect(search.has('query')).toBe(false);
    expect(search.has('page')).toBe(false);
    expect(search.has('pageSize')).toBe(false);
    expect(search.has('sort')).toBe(false);
    expect(search.get('marketplace')).toBe('Core');
  });

  it('writes every non-default browse value', () => {
    const url = buildBrowseUrl('/marketplace', '', {
      marketplace: 'Core',
      query: 'react',
      page: 3,
      pageSize: 48,
      sort: 'name-desc',
    });
    const search = new URL(url, 'http://localhost').searchParams;
    expect(search.get('marketplace')).toBe('Core');
    expect(search.get('query')).toBe('react');
    expect(search.get('page')).toBe('3');
    expect(search.get('pageSize')).toBe('48');
    expect(search.get('sort')).toBe('name-desc');
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
