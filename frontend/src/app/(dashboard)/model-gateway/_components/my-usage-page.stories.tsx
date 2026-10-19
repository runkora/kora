import type { Meta, StoryObj } from '@storybook/react';
import { dashboardPage } from '@runkora/storybook/dashboard-frame';
import type { MockRoutes } from '@runkora/storybook/mock-api';
import type { AnalyticsDays } from '../_types';
import { MyUsageShell } from '../my-usage/_components/my-usage-shell';
import {
  BUDGET_STATUS,
  BUDGET_STATUS_REACHED,
  MY_USAGE_7D,
  PATS,
  analyticsResponse,
  emptyAnalytics,
} from './gateway-mock-data';

/** Answers the period and key filter changes the page refetches on. */
const routes = (empty: boolean): MockRoutes => ({
  'GET /api/analytics/me': ({ url }: { url: URL }) => {
    const days = Number(url.searchParams.get('days') ?? 30) as AnalyticsDays;
    return empty ? emptyAnalytics('personal', days) : analyticsResponse('personal', days, 380);
  },
  'PUT /api/preferences': {},
});

/** Personal usage: budget ceilings above spend, tokens, requests and latency charts. */
const meta: Meta<typeof MyUsageShell> = {
  title: 'Pages/Models/My usage',
  component: MyUsageShell,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/model-gateway/my-usage' } },
  },
};

export default meta;
type Story = StoryObj<typeof MyUsageShell>;

const page = dashboardPage(routes(false));
const emptyPage = dashboardPage(routes(true));

const populated = {
  initialData: MY_USAGE_7D,
  initialError: null,
  pats: PATS,
  initialBudget: BUDGET_STATUS,
};

export const Default: Story = { args: populated, decorators: [page] };

export const NoBudget: Story = {
  decorators: [page],
  args: { ...populated, initialBudget: null },
};

export const BudgetReached: Story = {
  decorators: [page],
  args: { ...populated, initialBudget: BUDGET_STATUS_REACHED },
};

/** A period with no traffic: zeroed KPIs and empty charts. */
export const EmptyPeriod: Story = {
  args: { ...populated, initialData: emptyAnalytics('personal', 7) },
  decorators: [emptyPage],
};

export const NoKeys: Story = {
  decorators: [page],
  args: { ...populated, pats: [] },
};

export const LoadFailed: Story = {
  decorators: [page],
  args: {
    ...populated,
    initialData: null,
    initialError: 'Failed to reach inference gateway',
    initialBudget: null,
  },
};
