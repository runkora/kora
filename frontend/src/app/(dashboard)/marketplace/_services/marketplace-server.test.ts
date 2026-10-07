/**
 * Unit tests for the Marketplace choice in loadMarketplaceOverview.
 *
 * Order: the Marketplace in the URL, the saved Marketplace from the user
 * preferences, the first Marketplace in name order.
 *
 * Strategy:
 * - The Marketplace client serves its fixtures, so it runs unchanged. In name
 *   order, the fixture Marketplaces are "ClearRoute Core", "ClearRoute
 *   Platform", "Partner Reference", and "Platform Engineering".
 * - getPreferences is mocked, so each test sets the saved Marketplace.
 */
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { PREFERENCE_DEFAULTS } from '../../_preferences/preferences.schema';
import type { MarketplaceBrowseParams, MarketplaceOverviewData } from '../_types';
import { loadMarketplaceOverview } from './marketplace-server';

const { getPreferences } = vi.hoisted(() => ({ getPreferences: vi.fn() }));
vi.mock('../../_preferences/preferences-server', () => ({ getPreferences }));

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
});

describe('loadMarketplaceOverview Marketplace choice', () => {
  it('uses the first Marketplace on a first visit', async () => {
    saveMarketplace('');

    const overview = await loadOverview(DEFAULT_BROWSE);

    expect(overview.browse.marketplace).toBe('ClearRoute Core');
  });

  it('uses the saved Marketplace when the URL names no Marketplace', async () => {
    saveMarketplace('Partner Reference');

    const overview = await loadOverview(DEFAULT_BROWSE);

    expect(overview.browse.marketplace).toBe('Partner Reference');
    // The requested values stay equal to the URL, for the mount check of the hook.
    expect(overview.requested.marketplace).toBe('');
  });

  it('uses the Marketplace in the URL and does not read the preferences', async () => {
    saveMarketplace('Partner Reference');

    const overview = await loadOverview({ ...DEFAULT_BROWSE, marketplace: 'Platform Engineering' });

    expect(overview.browse.marketplace).toBe('Platform Engineering');
    expect(getPreferences).not.toHaveBeenCalled();
  });

  it('uses the first Marketplace when the saved Marketplace no longer exists', async () => {
    saveMarketplace('Removed Marketplace');

    const overview = await loadOverview(DEFAULT_BROWSE);

    expect(overview.browse.marketplace).toBe('ClearRoute Core');
  });
});
