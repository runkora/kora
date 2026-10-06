import type { Meta, StoryObj } from '@storybook/react';
import { ALL_NAV_SCOPES, dashboardPage, sessionRoutes } from '@orbit/storybook/dashboard-frame';
import type { MockRoutes } from '@orbit/storybook/mock-api';
import type { AnalyticsDays } from '../_types';
import { AnalyticsShell } from '../analytics/_components/analytics-shell';
import { MODEL_ROWS } from './model-mock-data';
import {
  EMPTY_PRINCIPALS_PAGE,
  PRINCIPALS_PAGE,
  TENANT_ANALYTICS_7D,
  analyticsResponse,
  emptyAnalytics,
} from './gateway-mock-data';

const routes = (empty: boolean): MockRoutes => ({
  ...sessionRoutes([...ALL_NAV_SCOPES, 'platform:analytics:read']),
  'GET /api/analytics': ({ url }: { url: URL }) => {
    const days = Number(url.searchParams.get('days') ?? 30) as AnalyticsDays;
    return empty ? emptyAnalytics('tenant', days) : analyticsResponse('tenant', days, 12_800);
  },
  'GET /api/analytics/principals': empty ? EMPTY_PRINCIPALS_PAGE : PRINCIPALS_PAGE,
  'PUT /api/preferences': {},
});

/** Tenant-wide analytics with an Overview of charts and a per-principal usage table. */
const meta: Meta<typeof AnalyticsShell> = {
  title: 'Pages/Models/Analytics',
  component: AnalyticsShell,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/model-gateway/analytics' } },
  },
};

export default meta;
type Story = StoryObj<typeof AnalyticsShell>;

const page = dashboardPage(routes(false));
const emptyPage = dashboardPage(routes(true));

const populated = {
  initialData: TENANT_ANALYTICS_7D,
  initialError: null,
  models: MODEL_ROWS,
};

export const Overview: Story = { args: populated, decorators: [page] };

export const Principals: Story = {
  decorators: [page],
  args: { ...populated, initialTab: 'principals' },
};

export const EmptyPeriod: Story = {
  args: { ...populated, initialData: emptyAnalytics('tenant', 7) },
  decorators: [emptyPage],
};

export const EmptyPrincipals: Story = {
  args: { ...populated, initialTab: 'principals' },
  decorators: [emptyPage],
};

export const LoadFailed: Story = {
  decorators: [page],
  args: { initialData: null, initialError: 'Failed to reach inference gateway', models: [] },
};
