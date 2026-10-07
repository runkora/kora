import type { Meta, StoryObj } from '@storybook/react';
import { dashboardPage } from '@orbit/storybook/dashboard-frame';
import { DashboardPage } from './dashboard-page';
import {
  ALERTING_WORKSPACE,
  BUSY_WORKSPACE,
  LIGHT_TRAFFIC,
  LIMIT_REACHED_WORKSPACE,
  NEW_WORKSPACE,
} from './dashboard-mock-data';

/** The dashboard home as an operator sees it, with mock workspace data. */
const meta: Meta<typeof DashboardPage> = {
  title: 'Pages/Dashboard',
  component: DashboardPage,
  decorators: [dashboardPage()],
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/' } },
  },
};

export default meta;
type Story = StoryObj<typeof DashboardPage>;

export const Default: Story = { args: BUSY_WORKSPACE };

/** Nothing registered yet: every section shows its empty state. */
export const NewWorkspace: Story = { args: NEW_WORKSPACE };

/** Spend close to the cap, failing runs and an elevated error rate. */
export const WithAlerts: Story = { args: ALERTING_WORKSPACE };

/** The tenant budget is exhausted. */
export const BudgetLimitReached: Story = { args: LIMIT_REACHED_WORKSPACE };

/** A small team with a few agents and little traffic. */
export const LightTraffic: Story = { args: LIGHT_TRAFFIC };
