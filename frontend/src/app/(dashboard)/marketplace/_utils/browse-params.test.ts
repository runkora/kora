/**
 * Unit tests for the Skill URL and the back link URL.
 *
 * A Skill URL carries the browse values of the overview. The Skill page reads
 * them back and builds its back link, so that the link returns to the same
 * results (PRD FR-17).
 */
import { describe, expect, it } from 'vitest';
import type { MarketplaceBrowseParams } from '../_types';
import { buildOverviewUrl, buildSkillUrl, parseBrowseParams } from './browse-params';

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
