import type { Meta, StoryObj } from '@storybook/react';
import { expect, userEvent, within } from 'storybook/test';
import { dashboardPage } from '@orbit/storybook/dashboard-frame';
import { ConnectionsPage } from './connections-page';
import { MANY_CONNECTIONS, MOCK_CONNECTIONS } from './connections-mock-data';

/** The read-only connections catalog as an operator sees it, with mock connections. */
const meta: Meta<typeof ConnectionsPage> = {
  title: 'Pages/Connections',
  component: ConnectionsPage,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/connections' } },
  },
  decorators: [dashboardPage()],
};

export default meta;
type Story = StoryObj<typeof ConnectionsPage>;

export const Default: Story = {
  args: { rows: MOCK_CONNECTIONS, error: null },
};

export const Empty: Story = {
  args: { rows: [], error: null },
};

export const LoadError: Story = {
  args: { rows: [], error: 'Failed to load connections: egress gateway unreachable (503).' },
};

export const Many: Story = {
  args: { rows: MANY_CONNECTIONS, error: null },
};

/** Search narrowed to the GitHub adapter. */
export const Filtered: Story = {
  args: { rows: MOCK_CONNECTIONS, error: null },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.type(await canvas.findByPlaceholderText('Search connections…'), 'github');
  },
};

/** The detail modal for the busiest connection, listing every grantable action. */
export const ConnectionDetailOpen: Story = {
  args: { rows: MOCK_CONNECTIONS, error: null },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(await canvas.findByLabelText('View details for github-orbital'));
    await expect(await within(document.body).findByRole('dialog')).toBeTruthy();
  },
};
