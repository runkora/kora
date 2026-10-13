import type { Meta, StoryObj } from '@storybook/react';
import { expect, userEvent, within } from 'storybook/test';
import { dashboardPage } from '@orbit/storybook/dashboard-frame';
import { McpServersShell } from './mcp-servers-shell';
import { MANY_SERVERS, MOCK_SERVERS } from './mcp-mock-data';

/** The MCP servers list as an operator sees it, with mock servers. */
const meta: Meta<typeof McpServersShell> = {
  title: 'Pages/MCP Servers',
  component: McpServersShell,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/mcp-gateway' } },
  },
  decorators: [dashboardPage()],
};

export default meta;
type Story = StoryObj<typeof McpServersShell>;

export const Default: Story = {
  args: { servers: MOCK_SERVERS, error: null },
};

export const Empty: Story = {
  args: { servers: [], error: null },
};

export const Error: Story = {
  args: { servers: [], error: 'Failed to load MCP servers' },
};

export const Many: Story = {
  args: { servers: MANY_SERVERS, error: null },
};

/** Two servers ticked and the Build config panel open. */
export const BuildConfigOpen: Story = {
  args: { servers: MOCK_SERVERS, error: null },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(await canvas.findByLabelText('Select GitHub'));
    await userEvent.click(await canvas.findByLabelText('Select Slack'));
    const buttons = await canvas.findAllByRole('button', { name: /Build config/ });
    await userEvent.click(buttons[buttons.length - 1]!);
    await expect(
      await within(document.body).findByText('one gateway endpoint', { exact: false }),
    ).toBeTruthy();
  },
};
