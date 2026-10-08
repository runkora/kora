/** @tier 3 — marketplace-scoped. Interactive preview of the result range and page controls. */

import type { Meta, StoryObj } from '@storybook/react';
import type { PackageOverview } from '@orbit/shared/api';
import { mockApi, type MockRequest } from '@orbit/storybook/mock-api';
import { PreferencesProvider } from '../../_preferences/preferences-context';
import { PREFERENCE_DEFAULTS } from '../../_preferences/preferences.schema';
import { MarketplaceProvider } from '../_context/marketplace-context';
import {
  DEFAULT_PAGE_SIZE,
  type MarketplaceBrowseParams,
  type MarketplaceOverviewData,
} from '../_types';
import { buildBrowseUrl, parseBrowseParams } from '../_utils/browse-params';
import { MarketplacePagination } from './marketplace-pagination';

const sampleSkills: PackageOverview[] = Array.from({ length: 240 }, (_, index) => ({
  slug: `platform-engineering/skills/sample-${index + 1}`,
  title_of_package: `Sample Skill ${index + 1}`,
  description: 'Sample Skill for the pagination preview.',
}));

function createOverview(
  requested: MarketplaceBrowseParams,
  skills: PackageOverview[] = sampleSkills,
): MarketplaceOverviewData {
  const browse = { ...requested, marketplace: 'Platform Engineering' };
  const offset = (browse.page - 1) * browse.pageSize;

  return {
    marketplaces: [{ name: browse.marketplace }],
    requested,
    browse,
    skills: skills.slice(offset, offset + browse.pageSize),
    total: skills.length,
  };
}

const meta: Meta<typeof MarketplacePagination> = {
  title: 'App/Marketplace/Pagination',
  component: MarketplacePagination,
  tags: ['autodocs'],
  parameters: {
    layout: 'padded',
    // Each Docs preview needs its own URL and fetch mocks.
    docs: { story: { inline: false, height: '180px' } },
  },
};
export default meta;
type Story = StoryObj<typeof MarketplacePagination>;

function createPageStory(page: number, skills: PackageOverview[] = sampleSkills): Story {
  const requested = { ...parseBrowseParams(new URLSearchParams()), page };
  const initialData = createOverview(requested, skills);

  return {
    beforeEach: () => {
      const previousBrowse = parseBrowseParams(new URLSearchParams(window.location.search));
      const previousState = window.history.state;

      // The real hook compares its initial data with the preview URL on mount.
      window.history.replaceState(
        previousState,
        '',
        buildBrowseUrl(window.location.pathname, window.location.search, initialData.browse),
      );

      return () => {
        window.history.replaceState(
          previousState,
          '',
          buildBrowseUrl(window.location.pathname, window.location.search, previousBrowse),
        );
      };
    },
    decorators: [
      mockApi({
        'GET /api/marketplace/skills': ({ url }: MockRequest) => ({
          data: createOverview(parseBrowseParams(url.searchParams), skills),
        }),
        'PUT /api/preferences': {},
      }),
      (Story) => (
        <PreferencesProvider initialPreferences={PREFERENCE_DEFAULTS}>
          <MarketplaceProvider initialData={initialData} initialError={null}>
            <Story />
          </MarketplaceProvider>
        </PreferencesProvider>
      ),
    ],
  };
}

export const Default: Story = createPageStory(1);

export const FirstPage: Story = createPageStory(1);

export const MiddlePage: Story = createPageStory(5);

export const LastPage: Story = createPageStory(Math.ceil(sampleSkills.length / DEFAULT_PAGE_SIZE));

export const ZeroResults: Story = createPageStory(1, []);
