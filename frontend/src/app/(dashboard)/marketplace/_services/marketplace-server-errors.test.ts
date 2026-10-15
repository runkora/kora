/**
 * Unit tests for marketplace-server.ts error/edge-case paths that need the
 * Marketplace client mocked (real fixtures can't produce zero Marketplaces,
 * forced pagination edges, cancellations, or upstream errors). Kept in a
 * separate file from marketplace-server.test.ts, which intentionally runs
 * against real fixtures — a file-scoped vi.mock here must not leak there.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('@orbit/server/marketplace-client', () => ({
  fetchMarketplaces: vi.fn(),
  fetchSkills: vi.fn(),
  fetchSkillDetail: vi.fn(),
}));

vi.mock('../../_preferences/preferences-server', () => ({
  getPreferences: vi.fn(),
}));

import { loadMarketplaceOverview, loadSkillDetail } from './marketplace-server';
import { fetchMarketplaces, fetchSkillDetail, fetchSkills } from '@orbit/server/marketplace-client';
import { getPreferences } from '../../_preferences/preferences-server';
import { ApiError } from '@orbit/server/api-error';
import type { Skill } from '@orbit/shared/api';
import type { MarketplaceBrowseParams } from '../_types';
import type { UserPreferences } from '../../_preferences/preferences.schema';

const ACCESS_TOKEN = 'test-token';

function browse(overrides: Partial<MarketplaceBrowseParams> = {}): MarketplaceBrowseParams {
  return { marketplace: '', query: '', page: 1, pageSize: 24, sort: 'name-asc', ...overrides };
}

function preferencesWith(lastSelected: string): UserPreferences {
  return { marketplace: { lastSelected } } as unknown as UserPreferences;
}

beforeEach(() => {
  vi.mocked(fetchMarketplaces).mockReset();
  vi.mocked(fetchSkills).mockReset();
  vi.mocked(fetchSkillDetail).mockReset();
  vi.mocked(getPreferences).mockReset();
  vi.mocked(getPreferences).mockResolvedValue(preferencesWith(''));
  vi.mocked(fetchSkills).mockResolvedValue({ skills: [] });
});

describe('zero Marketplaces', () => {
  it('returns ok with no Skills and no fetchSkills call when there are zero Marketplaces', async () => {
    vi.mocked(fetchMarketplaces).mockResolvedValue({ marketplaces: [] });

    const result = await loadMarketplaceOverview(ACCESS_TOKEN, browse());

    expect(result.ok).toBe(true);
    if (!result.ok) throw new Error('expected ok');
    expect(result.overview.marketplaces).toEqual([]);
    expect(result.overview.skills).toEqual([]);
    expect(result.overview.total).toBe(0);
    expect(result.overview.browse.marketplace).toBe('');
    expect(fetchSkills).not.toHaveBeenCalled();
  });
});

describe('Skills page', () => {
  it('returns empty Skills when the selected Marketplace has none', async () => {
    vi.mocked(fetchMarketplaces).mockResolvedValue({
      marketplaces: [
        { name: 'Solo', marketplaceSlug: 'Solo', link: 'https://github.com/example/solo' },
      ],
    });
    vi.mocked(fetchSkills).mockResolvedValue({ skills: [] });

    const result = await loadMarketplaceOverview(ACCESS_TOKEN, browse({ marketplace: 'Solo' }));

    expect(result.ok).toBe(true);
    if (!result.ok) throw new Error('expected ok');
    expect(result.overview.skills).toEqual([]);
    expect(result.overview.total).toBe(0);
  });

  it('forwards the Marketplace slug and the search to fetchSkills', async () => {
    vi.mocked(fetchMarketplaces).mockResolvedValue({
      marketplaces: [
        { name: 'Solo', marketplaceSlug: 'Solo', link: 'https://github.com/example/solo' },
      ],
    });

    await loadMarketplaceOverview(
      ACCESS_TOKEN,
      browse({ marketplace: 'Solo', query: 'react', sort: 'name-desc', page: 2, pageSize: 12 }),
    );

    expect(fetchSkills).toHaveBeenCalledWith(ACCESS_TOKEN, 'Solo', 'react', undefined);
  });

  it('sorts by name, cuts the page, and counts the total from the whole list', async () => {
    vi.mocked(fetchMarketplaces).mockResolvedValue({
      marketplaces: [
        { name: 'Solo', marketplaceSlug: 'Solo', link: 'https://github.com/example/solo' },
      ],
    });
    // The skills-service sorts by skillSlug, not by name.
    vi.mocked(fetchSkills).mockResolvedValue({
      skills: ['Charlie', 'alpha', 'Bravo', 'delta', 'Echo'].map((name) => ({
        name,
        description: '',
        skillSlug: `solo__${name.toLowerCase()}`,
      })),
    });

    const ascending = await loadMarketplaceOverview(
      ACCESS_TOKEN,
      browse({ marketplace: 'Solo', page: 2, pageSize: 2 }),
    );
    const descending = await loadMarketplaceOverview(
      ACCESS_TOKEN,
      browse({ marketplace: 'Solo', sort: 'name-desc', page: 1, pageSize: 2 }),
    );

    if (!ascending.ok || !descending.ok) throw new Error('expected ok');
    expect(ascending.overview.skills.map((skill) => skill.name)).toEqual(['Charlie', 'delta']);
    expect(ascending.overview.total).toBe(5);
    expect(descending.overview.skills.map((skill) => skill.name)).toEqual(['Echo', 'delta']);
  });

  it('clamps a page beyond the last page to the last page', async () => {
    vi.mocked(fetchMarketplaces).mockResolvedValue({
      marketplaces: [
        { name: 'Solo', marketplaceSlug: 'Solo', link: 'https://github.com/example/solo' },
      ],
    });
    vi.mocked(fetchSkills).mockResolvedValue({
      skills: Array.from({ length: 30 }, (_, index) => ({
        name: `Skill ${String(index).padStart(2, '0')}`,
        description: '',
        skillSlug: `solo__skill-${index}`,
      })),
    });

    const result = await loadMarketplaceOverview(
      ACCESS_TOKEN,
      browse({ marketplace: 'Solo', page: 5, pageSize: 24 }),
    );

    expect(result.ok).toBe(true);
    if (!result.ok) throw new Error('expected ok');
    expect(result.overview.browse.page).toBe(2);
    expect(result.overview.skills).toHaveLength(6);
    expect(result.overview.total).toBe(30);
    // The whole list arrives in one request, so the last page needs no second request.
    expect(fetchSkills).toHaveBeenCalledTimes(1);
  });
});

describe('cancellation', () => {
  it('treats an aborted Marketplace request as a cancellation, not a failure', async () => {
    const controller = new AbortController();
    controller.abort();
    vi.mocked(fetchMarketplaces).mockRejectedValue(new DOMException('aborted', 'AbortError'));

    const result = await loadMarketplaceOverview(ACCESS_TOKEN, browse(), controller.signal);

    expect(result).toEqual({ ok: false, status: 499, message: 'Request cancelled' });
  });

  it('treats an aborted Skills request as a cancellation, not a failure', async () => {
    const controller = new AbortController();
    vi.mocked(fetchMarketplaces).mockResolvedValue({
      marketplaces: [
        { name: 'Solo', marketplaceSlug: 'Solo', link: 'https://github.com/example/solo' },
      ],
    });
    vi.mocked(fetchSkills).mockImplementation(async () => {
      controller.abort();
      throw new DOMException('aborted', 'AbortError');
    });

    const result = await loadMarketplaceOverview(
      ACCESS_TOKEN,
      browse({ marketplace: 'Solo' }),
      controller.signal,
    );

    // The Marketplace list already loaded, so it is retained on this partial
    // failure (see MarketplaceOverviewResult's `overview` field); only the
    // ok/status/message triple matters for this cancellation assertion.
    expect(result).toMatchObject({ ok: false, status: 499, message: 'Request cancelled' });
  });
});

describe('error mapping', () => {
  it('maps an ApiError from fetchMarketplaces to its upstream status and message', async () => {
    vi.mocked(fetchMarketplaces).mockRejectedValue(new ApiError(403, 'forbidden', 'No access'));

    const result = await loadMarketplaceOverview(ACCESS_TOKEN, browse());

    expect(result).toEqual({ ok: false, status: 403, message: 'No access' });
  });

  it('maps a non-ApiError failure from fetchMarketplaces to 502', async () => {
    vi.mocked(fetchMarketplaces).mockRejectedValue(new Error('boom'));

    const result = await loadMarketplaceOverview(ACCESS_TOKEN, browse());

    expect(result).toEqual({ ok: false, status: 502, message: 'Failed to load Marketplaces' });
  });

  it('maps an ApiError from fetchSkills to its upstream status and message', async () => {
    vi.mocked(fetchMarketplaces).mockResolvedValue({
      marketplaces: [
        { name: 'Solo', marketplaceSlug: 'Solo', link: 'https://github.com/example/solo' },
      ],
    });
    vi.mocked(fetchSkills).mockRejectedValue(new ApiError(503, 'unavailable', 'Service down'));

    const result = await loadMarketplaceOverview(ACCESS_TOKEN, browse({ marketplace: 'Solo' }));

    // The Marketplace list already loaded, so it is retained on this partial
    // failure (see MarketplaceOverviewResult's `overview` field); only the
    // ok/status/message triple matters for this error-mapping assertion.
    expect(result).toMatchObject({ ok: false, status: 503, message: 'Service down' });
  });

  it('maps a non-ApiError failure from fetchSkills to 502', async () => {
    vi.mocked(fetchMarketplaces).mockResolvedValue({
      marketplaces: [
        { name: 'Solo', marketplaceSlug: 'Solo', link: 'https://github.com/example/solo' },
      ],
    });
    vi.mocked(fetchSkills).mockRejectedValue(new Error('boom'));

    const result = await loadMarketplaceOverview(ACCESS_TOKEN, browse({ marketplace: 'Solo' }));

    // The Marketplace list already loaded, so it is retained on this partial
    // failure (see MarketplaceOverviewResult's `overview` field); only the
    // ok/status/message triple matters for this error-mapping assertion.
    expect(result).toMatchObject({ ok: false, status: 502, message: 'Failed to load Skills' });
  });
});

describe('loadSkillDetail', () => {
  function makeSkill(overrides: Partial<Skill> = {}): Skill {
    return {
      skillSlug: 'core__skills__react-expert',
      name: 'React Expert',
      description: 'Senior React developer.',
      skill: { path: 'skills/react-expert/SKILL.md', name: 'SKILL.md', content: '# React Expert' },
      documents: [],
      ...overrides,
    };
  }

  it('returns the Skill for a known slug', async () => {
    const skill = makeSkill();
    vi.mocked(fetchSkillDetail).mockResolvedValue(skill);

    const result = await loadSkillDetail(ACCESS_TOKEN, 'core__skills', skill.skillSlug);

    expect(result).toEqual({ ok: true, skill });
    expect(fetchSkillDetail).toHaveBeenCalledWith(ACCESS_TOKEN, 'core__skills', skill.skillSlug);
  });

  it('returns a 404 with a message distinct from a failed load for a missing Skill', async () => {
    vi.mocked(fetchSkillDetail).mockResolvedValue(null);

    const result = await loadSkillDetail(
      ACCESS_TOKEN,
      'core__skills',
      'core__skills__does-not-exist',
    );

    expect(result).toEqual({ ok: false, status: 404, message: 'Skill not found' });
  });

  it('maps an upstream ApiError status for a failed Skill load', async () => {
    vi.mocked(fetchSkillDetail).mockRejectedValue(
      new ApiError(500, 'internal', 'Upstream failure'),
    );

    const result = await loadSkillDetail(
      ACCESS_TOKEN,
      'core__skills',
      'core__skills__react-expert',
    );

    expect(result).toEqual({ ok: false, status: 500, message: 'Upstream failure' });
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.status).not.toBe(404);
  });

  it('maps an unexpected failure to 502', async () => {
    vi.mocked(fetchSkillDetail).mockRejectedValue(new Error('boom'));

    const result = await loadSkillDetail(
      ACCESS_TOKEN,
      'core__skills',
      'core__skills__react-expert',
    );

    expect(result).toEqual({ ok: false, status: 502, message: 'Failed to load the Skill' });
  });
});
