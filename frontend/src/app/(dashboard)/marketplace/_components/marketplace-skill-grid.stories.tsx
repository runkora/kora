/** @tier 3 — marketplace-scoped. Preview of Skill detail links and their responsive card layout. */

import type { Meta, StoryObj } from '@storybook/react';
import { mockApi } from '@orbit/storybook/mock-api';
import { PreferencesProvider } from '../../_preferences/preferences-context';
import { PREFERENCE_DEFAULTS } from '../../_preferences/preferences.schema';
import { MarketplaceProvider } from '../_context/marketplace-context';
import type { MarketplaceOverviewData } from '../_types';
import { parseBrowseParams } from '../_utils/browse-params';
import { MarketplaceSkillGrid } from './marketplace-skill-grid';
import { marketplaceStorySkills, marketplaceStoryPlatform } from './marketplace-story-fixtures';

const requested = parseBrowseParams(new URLSearchParams());
const initialData: MarketplaceOverviewData = {
  marketplaces: [marketplaceStoryPlatform],
  requested,
  browse: { ...requested, marketplace: marketplaceStoryPlatform.marketplaceSlug },
  skills: marketplaceStorySkills,
  total: marketplaceStorySkills.length,
};

const meta: Meta<typeof MarketplaceSkillGrid> = {
  title: 'Pages/Marketplace/SkillGrid',
  component: MarketplaceSkillGrid,
  tags: ['autodocs'],
  parameters: {
    layout: 'padded',
    nextjs: { appDirectory: true },
  },
  decorators: [
    mockApi({
      'GET /api/marketplace/skills': { data: initialData },
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
export default meta;
type Story = StoryObj<typeof MarketplaceSkillGrid>;

export const Default: Story = {};
