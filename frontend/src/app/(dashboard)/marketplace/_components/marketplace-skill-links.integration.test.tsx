/**
 * Integration tests for the Skill links of the overview.
 *
 * A Skill link carries the browse values of the results on screen, so that
 * the back link on the Skill page returns to the same results (PRD FR-17).
 *
 * Strategy:
 * - A real MarketplaceProvider serves the context. The saved Marketplace in
 *   the preferences equals the Marketplace on screen, so no PUT occurs.
 * - The jsdom URL matches the server render, so the hook sends no BFF request.
 * - `next/navigation` is mocked, so the test can read the list row navigation.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '@orbit/test-utils';
import { PreferencesProvider } from '../../_preferences/preferences-context';
import { PREFERENCE_DEFAULTS, type UserPreferences } from '../../_preferences/preferences.schema';
import { MarketplaceProvider } from '../_context/marketplace-context';
import type { MarketplaceBrowseParams, MarketplaceOverviewData } from '../_types';
import { MarketplaceSkillGrid } from './marketplace-skill-grid';
import { MarketplaceSkillList } from './marketplace-skill-list';

vi.mock('next/navigation', () => ({
  useRouter: vi.fn(),
}));

import { useRouter } from 'next/navigation';

// ── Fixtures ──────────────────────────────────────────────────────────────────

/** Page 3 of a search in "Platform Engineering", with other values that are not defaults. */
const PAGE_3_BROWSE: MarketplaceBrowseParams = {
  marketplace: 'Platform Engineering',
  query: 'review',
  page: 3,
  pageSize: 12,
  sort: 'name-desc',
};

const PAGE_3_RENDER: MarketplaceOverviewData = {
  marketplaces: [{ name: 'ClearRoute Core' }, { name: 'Platform Engineering' }],
  requested: PAGE_3_BROWSE,
  browse: PAGE_3_BROWSE,
  skills: [
    {
      slug: 'platform-engineering/skills/react-reviewer',
      title_of_package: 'React Reviewer',
      description: 'Reviews React components.',
    },
  ],
  total: 30,
};

const PAGE_3_QUERY =
  'marketplace=Platform+Engineering&query=review&page=3&pageSize=12&sort=name-desc';

const SKILL_URL = `/marketplace/platform-engineering%2Fskills%2Freact-reviewer?${PAGE_3_QUERY}`;

/** The saved Marketplace equals the Marketplace on screen, so the provider sends no PUT. */
const SAVED_PREFERENCES: UserPreferences = {
  ...PREFERENCE_DEFAULTS,
  marketplace: { lastSelected: 'Platform Engineering' },
};

// ── Helpers ───────────────────────────────────────────────────────────────────

function renderInMarketplace(ui: React.ReactElement) {
  return renderWithProviders(
    <PreferencesProvider initialPreferences={SAVED_PREFERENCES}>
      <MarketplaceProvider initialData={PAGE_3_RENDER} initialError={null}>
        {ui}
      </MarketplaceProvider>
    </PreferencesProvider>,
  );
}

beforeEach(() => {
  window.history.replaceState(null, '', `/marketplace?${PAGE_3_QUERY}`);
});

afterEach(() => {
  window.history.replaceState(null, '', '/');
});

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('Skill links of the overview', () => {
  it('card view: the card links to the Skill URL with the browse values', () => {
    renderInMarketplace(<MarketplaceSkillGrid />);

    expect(screen.getByRole('link', { name: /React Reviewer/ })).toHaveAttribute('href', SKILL_URL);
  });

  it('list view: a row click opens the Skill URL with the browse values', async () => {
    const push = vi.fn();
    vi.mocked(useRouter).mockReturnValue({ push } as unknown as ReturnType<typeof useRouter>);

    renderInMarketplace(<MarketplaceSkillList />);

    const user = userEvent.setup();
    await user.click(screen.getByRole('row', { name: 'Open React Reviewer' }));

    expect(push).toHaveBeenCalledWith(SKILL_URL);
  });
});
