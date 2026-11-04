/** @tier 3 — skills-scoped. The Skills overview in each of its states. */

import type { Meta, StoryObj } from '@storybook/react';
import { mockApi } from '@runkora/storybook/mock-api';
import { PreferencesProvider } from '../../_preferences/preferences-context';
import { PREFERENCE_DEFAULTS } from '../../_preferences/preferences.schema';
import type { SkillsLoadError, SkillsOverview } from '../_types';
import { SkillsShell } from './skills-shell';
import { storyOverview, storyPage, TEAM_MARKETPLACE } from './skills-story-fixtures';

const meta: Meta<typeof SkillsShell> = {
  title: 'Pages/Skills/Overview',
  component: SkillsShell,
  tags: ['autodocs'],
  parameters: { layout: 'padded', nextjs: { appDirectory: true } },
  // Every browse change gets the same fixture page; the stories show states, not paging.
  decorators: [
    mockApi({ 'GET /api/skills': { data: storyPage() } }),
    (Story) => (
      <PreferencesProvider initialPreferences={PREFERENCE_DEFAULTS}>
        <Story />
      </PreferencesProvider>
    ),
  ],
};
export default meta;
type Story = StoryObj<typeof SkillsShell>;

function story(
  overrides: Partial<SkillsOverview> = {},
  error: SkillsLoadError | null = null,
): Story {
  return { args: { initialOverview: storyOverview(overrides), initialError: error } };
}

export const Default: Story = story();

export const NoSearchMatches: Story = story({
  browse: { marketplace: TEAM_MARKETPLACE.slug, search: 'nothing', page: 1 },
  skills: [],
  total: 0,
});

export const EmptyMarketplace: Story = story({ skills: [], total: 0 });

export const UnavailableMarketplace: Story = story(
  { skills: [], total: 0 },
  { status: 502, message: 'The GitHub App credential for this Marketplace is unavailable.' },
);

export const NoMarketplaces: Story = story({ marketplaces: [], skills: [], total: 0 });
