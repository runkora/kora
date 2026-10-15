/** @tier 3 — marketplace-scoped. Preview of Skill cards that open in place, and their responsive layout. */

import type { Meta, StoryObj } from '@storybook/react';
import { mockApi, type MockRequest } from '@orbit/storybook/mock-api';
import { PreferencesProvider } from '../../_preferences/preferences-context';
import { PREFERENCE_DEFAULTS } from '../../_preferences/preferences.schema';
import { MarketplaceProvider } from '../_context/marketplace-context';
import type { MarketplaceOverviewData } from '../_types';
import { parseBrowseParams } from '../_utils/browse-params';
import { MarketplaceSkillGrid } from './marketplace-skill-grid';
import { marketplaceStorySkills, marketplaceStorySkillDetail } from './marketplace-story-fixtures';

const requested = parseBrowseParams(new URLSearchParams());
const initialData: MarketplaceOverviewData = {
  marketplaces: [
    {
      name: 'Platform Engineering',
      marketplaceSlug: 'Platform Engineering',
      link: 'https://github.com/example/platform-engineering',
    },
  ],
  requested,
  browse: { ...requested, marketplace: 'Platform Engineering' },
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
      'GET /api/marketplace/skill': ({ url }: MockRequest) => ({
        data: marketplaceStorySkillDetail(url.searchParams.get('skill') ?? ''),
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
export default meta;
type Story = StoryObj<typeof MarketplaceSkillGrid>;

export const Default: Story = {};
