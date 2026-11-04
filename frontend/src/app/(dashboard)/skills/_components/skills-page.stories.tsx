/** @tier 3 — skills-scoped. The Skills overview in each of its states, fetching later pages through the mocked BFF. */

import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, within } from 'storybook/test';
import { dashboardPage } from '@runkora/storybook/dashboard-frame';
import type { MockRequest } from '@runkora/storybook/mock-api';
import { PreferencesProvider } from '../../_preferences/preferences-context';
import { PREFERENCE_DEFAULTS } from '../../_preferences/preferences.schema';
import { toBrowse } from '../_types';
import { SkillsShell } from './skills-shell';
import { skillsPage, STORY_FAVOURITES, storyOverview } from './skills-story-fixtures';

const UNAVAILABLE = 'This Marketplace could not be loaded. Try again later.';

const meta: Meta<typeof SkillsShell> = {
  title: 'Pages/Skills/Overview',
  component: SkillsShell,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/skills' } },
  },
  decorators: [
    // The user's favourites, as the preferences document would return them.
    (Story) => (
      <PreferencesProvider
        initialPreferences={{
          ...PREFERENCE_DEFAULTS,
          skills: { favouriteSkills: STORY_FAVOURITES },
        }}
      >
        <Story />
      </PreferencesProvider>
    ),
    dashboardPage({
      'GET /api/skills': ({ url }: MockRequest) => {
        const query = Object.fromEntries(url.searchParams);
        return { data: skillsPage(toBrowse(query), (query.keys ?? '').split(',')) };
      },
      'PUT /api/preferences': {},
    }),
  ],
  args: { initialOverview: storyOverview(), initialError: null },
};
export default meta;
type Story = StoryObj<typeof SkillsShell>;

export const AllMarketplaces: Story = {};

export const OneMarketplace: Story = {
  args: { initialOverview: storyOverview({ marketplace: 'anthropic-skills' }) },
};

export const ListView: Story = {
  args: { initialOverview: storyOverview({ marketplace: 'anthropic-skills' }) },
  play: async ({ canvasElement }) => {
    await userEvent.click(await within(canvasElement).findByRole('button', { name: 'List view' }));
  },
};

export const Favourites: Story = {
  args: { initialOverview: storyOverview({ favourites: true }, STORY_FAVOURITES) },
};

export const NoSearchMatches: Story = {
  args: { initialOverview: storyOverview({ search: 'nothing-matches' }) },
};

export const SomeMarketplacesUnavailable: Story = {
  args: {
    initialOverview: {
      ...storyOverview(),
      failures: [{ marketplace: 'mattpocock-skills', message: UNAVAILABLE }],
    },
  },
};

export const UnavailableMarketplace: Story = {
  args: {
    initialOverview: {
      ...storyOverview({ marketplace: 'anthropic-skills' }),
      skills: [],
      total: 0,
    },
    initialError: { status: 502, message: UNAVAILABLE },
  },
};

export const NoMarketplaces: Story = {
  args: { initialOverview: { ...storyOverview(), marketplaces: [], skills: [], total: 0 } },
};
