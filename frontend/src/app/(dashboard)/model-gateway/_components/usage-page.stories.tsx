import type { Meta, StoryObj } from '@storybook/react';
import { dashboardPage } from '@orbit/storybook/dashboard-frame';
import ModelGatewayLayout from '../layout';
import { UsageTab } from '../usage/_components/usage-tab';

/** The legacy usage view. It renders from built-in sample data, so it takes no props. */
const meta: Meta<typeof UsageTab> = {
  title: 'Pages/Models/Usage',
  component: UsageTab,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/model-gateway/usage' } },
  },
  decorators: [dashboardPage({}, ModelGatewayLayout)],
};

export default meta;
type Story = StoryObj<typeof UsageTab>;

export const Default: Story = {};
