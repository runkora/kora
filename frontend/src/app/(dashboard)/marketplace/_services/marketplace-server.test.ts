/**
 * Unit tests for the Marketplace choice in loadMarketplaceOverview.
 *
 * Order: the requested Marketplace, the saved Marketplace from the user
 * preferences, the first Marketplace in the skills-service order. Each choice
 * is a marketplaceSlug.
 *
 * Strategy:
 * - The Marketplace client is mocked (it now calls the live skills-service),
 *   so fetchMarketplaces/fetchSkills return fixed sample data below. In that
 *   sample order, the Marketplaces are "ClearRoute Core"
 *   (clearroute__core-skills), "ClearRoute Platform", "Partner Reference"
 *   (partner-org__reference-skills), and "Platform Engineering"
 *   (platform-engineering__skills).
 * - getPreferences is mocked, so each test sets the saved Marketplace.
 */
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { PREFERENCE_DEFAULTS } from '../../_preferences/preferences.schema';
import type { MarketplaceBrowseParams, MarketplaceOverviewData } from '../_types';

const CORE = 'clearroute__core-skills';
const PARTNER = 'partner-org__reference-skills';
const PLATFORM = 'platform-engineering__skills';

const { getPreferences, fetchMarketplaces, fetchSkills } = vi.hoisted(() => ({
  getPreferences: vi.fn(),
  fetchMarketplaces: vi.fn(),
  fetchSkills: vi.fn(),
}));
vi.mock('../../_preferences/preferences-server', () => ({ getPreferences }));
vi.mock('@orbit/server/marketplace-client', () => ({
  fetchMarketplaces,
  fetchSkills,
  fetchSkillDetail: vi.fn(),
}));

import { loadMarketplaceOverview } from './marketplace-server';

/** The browse values of `/marketplace` with no query keys. */
const DEFAULT_BROWSE: MarketplaceBrowseParams = {
  marketplace: '',
  query: '',
  page: 1,
  pageSize: 24,
  sort: 'name-asc',
};

function saveMarketplace(lastSelected: string) {
  getPreferences.mockResolvedValue({ ...PREFERENCE_DEFAULTS, marketplace: { lastSelected } });
}

async function loadOverview(browse: MarketplaceBrowseParams): Promise<MarketplaceOverviewData> {
  const result = await loadMarketplaceOverview('test-token', browse);
  if (!result.ok) {
    throw new Error(`Expected a successful load, got ${result.status}: ${result.message}`);
  }
  return result.overview;
}

beforeEach(() => {
  getPreferences.mockReset();
  fetchMarketplaces.mockReset().mockResolvedValue({
    marketplaces: [
      {
        name: 'ClearRoute Core',
        marketplaceSlug: CORE,
        link: 'https://github.com/clearroute/core-skills',
      },
      {
        name: 'ClearRoute Platform',
        marketplaceSlug: 'clearroute__platform-skills',
        link: 'https://github.com/clearroute/platform-skills',
      },
      {
        name: 'Partner Reference',
        marketplaceSlug: PARTNER,
        link: 'https://github.com/partner-org/reference-skills',
      },
      {
        name: 'Platform Engineering',
        marketplaceSlug: PLATFORM,
        link: 'https://github.com/platform-engineering/skills',
      },
    ],
  });
  fetchSkills.mockReset().mockResolvedValue({ skills: [] });
});

describe('loadMarketplaceOverview Marketplace choice', () => {
  it('uses the first Marketplace on a first visit', async () => {
    saveMarketplace('');

    const overview = await loadOverview(DEFAULT_BROWSE);

    expect(overview.browse.marketplace).toBe(CORE);
  });

  it('uses the saved Marketplace when the browse request names no Marketplace', async () => {
    saveMarketplace(PARTNER);

    const overview = await loadOverview(DEFAULT_BROWSE);

    expect(overview.browse.marketplace).toBe(PARTNER);
    // Keep the requested values distinct from the resolved server selection.
    expect(overview.requested.marketplace).toBe('');
  });

  it('uses the requested Marketplace slug and does not read the preferences', async () => {
    saveMarketplace(PARTNER);

    const overview = await loadOverview({ ...DEFAULT_BROWSE, marketplace: PLATFORM });

    expect(overview.browse.marketplace).toBe(PLATFORM);
    expect(getPreferences).not.toHaveBeenCalled();
  });

  it('uses the first Marketplace when the saved Marketplace no longer exists', async () => {
    saveMarketplace('removed__marketplace');

    const overview = await loadOverview(DEFAULT_BROWSE);

    expect(overview.browse.marketplace).toBe(CORE);
  });

  it('keeps the skills-service order of the Marketplaces', async () => {
    saveMarketplace('');

    const overview = await loadOverview(DEFAULT_BROWSE);

    expect(overview.marketplaces.map((m) => m.name)).toEqual([
      'ClearRoute Core',
      'ClearRoute Platform',
      'Partner Reference',
      'Platform Engineering',
    ]);
  });

  it('falls back to the first Marketplace for an unknown requested slug', async () => {
    saveMarketplace('');

    const overview = await loadOverview({
      ...DEFAULT_BROWSE,
      marketplace: 'nobody__nothing',
    });

    expect(overview.browse.marketplace).toBe(CORE);
  });
});
