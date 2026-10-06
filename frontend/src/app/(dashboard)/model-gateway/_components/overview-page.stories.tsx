import type { Meta, StoryObj } from '@storybook/react';
import { dashboardPage } from '@orbit/storybook/dashboard-frame';
import { OverviewTab } from './overview-tab';
import {
  ANALYTICS_BUCKETS,
  ANALYTICS_KPIS,
  COST_BY_MODEL,
  MODEL_ROWS,
  RECENT_USAGE,
} from './model-mock-data';

/** The Model Gateway landing page: traffic, spend, model health and recent requests. */
const meta: Meta<typeof OverviewTab> = {
  title: 'Pages/Models/Overview',
  component: OverviewTab,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/model-gateway/overview' } },
  },
  decorators: [dashboardPage()],
};

export default meta;
type Story = StoryObj<typeof OverviewTab>;

export const Default: Story = {
  args: {
    models: MODEL_ROWS,
    data: { kpis: ANALYTICS_KPIS, buckets: ANALYTICS_BUCKETS, costByModel: COST_BY_MODEL },
    recentRecords: RECENT_USAGE,
  },
};

export const NoTraffic: Story = {
  args: { models: MODEL_ROWS, data: null, recentRecords: [] },
};
