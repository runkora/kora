import type { Meta, StoryObj } from '@storybook/react';
import { OverviewTab } from './overview-tab';
import {
  RECENT_GRANTS,
  ROLES,
  ROUTE_REGISTRATIONS,
  STATS,
  STATS_EMPTY,
  STATS_STALE,
  VOCABULARY,
} from './policy-mock-data';
import { policyParameters, withPolicy } from './policy-story-setup';

const meta: Meta<typeof OverviewTab> = {
  title: 'Pages/Policy/Overview',
  component: OverviewTab,
  parameters: policyParameters('/policy/overview'),
  decorators: withPolicy(),
};

export default meta;
type Story = StoryObj<typeof OverviewTab>;

export const Default: Story = {
  args: {
    stats: STATS,
    recentGrants: RECENT_GRANTS,
    roles: ROLES,
    vocabulary: VOCABULARY,
    routes: ROUTE_REGISTRATIONS,
  },
};

/** One tenant's OPA bundle is behind, so the header badge and bundle list turn amber. */
export const StaleBundles: Story = {
  args: { ...Default.args, stats: STATS_STALE },
};

/** A new tenant with nothing registered yet. */
export const Empty: Story = {
  args: { stats: STATS_EMPTY, recentGrants: [], roles: [], vocabulary: [], routes: [] },
};

/** The stats call failed, so the page shows only the error. */
export const LoadError: Story = {
  args: { stats: null, recentGrants: [], roles: [], vocabulary: [], routes: [] },
};
