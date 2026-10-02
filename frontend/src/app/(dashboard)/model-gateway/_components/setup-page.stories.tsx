import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, within } from 'storybook/test';
import { dashboardPage } from '@orbit/storybook/dashboard-frame';
import ModelGatewayLayout from '../layout';
import { SetupTab } from './setup-tab';

/** Setup guide: provision a key, then point Claude Code, Codex, OpenCode or VS Code at the gateway. */
const meta: Meta<typeof SetupTab> = {
  title: 'Pages/Models/Setup',
  component: SetupTab,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/model-gateway/setup' } },
  },
  decorators: [dashboardPage({}, ModelGatewayLayout)],
};

export default meta;
type Story = StoryObj<typeof SetupTab>;

export const Default: Story = {};

/** Opens the collapsed "Provision an API key" steps. */
export const ProvisionKeyExpanded: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(await canvas.findByRole('button', { name: 'Expand section' }));
  },
};

export const CodexCli: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(await canvas.findByRole('tab', { name: 'Codex CLI' }));
  },
};

export const VsCode: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(await canvas.findByRole('tab', { name: 'VS Code' }));
  },
};
