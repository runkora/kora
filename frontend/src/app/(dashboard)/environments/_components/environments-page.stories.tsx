import type { Meta, StoryObj } from '@storybook/react';
import { expect, userEvent, within } from 'storybook/test';
import { dashboardPage } from '@orbit/storybook/dashboard-frame';
import { EnvironmentsPage } from './environments-page';
import {
  MANY_ENVIRONMENTS,
  MOCK_CATALOG,
  MOCK_ENVIRONMENTS,
  MOCK_IMAGES,
  MOCK_SIZES,
  previewHandler,
} from './environments-mock-data';

/** The environments list and its create/edit wizard, with mock environments and a mock preview endpoint. */
const meta: Meta<typeof EnvironmentsPage> = {
  title: 'Pages/Environments',
  component: EnvironmentsPage,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/environments' } },
  },
  decorators: [
    dashboardPage({
      'POST /api/agents/types/effective-access': previewHandler,
      'DELETE /api/agents/environments/:id': { ok: true },
    }),
  ],
  args: {
    rows: MOCK_ENVIRONMENTS,
    connections: MOCK_CATALOG,
    images: MOCK_IMAGES,
    sizes: MOCK_SIZES,
    error: null,
  },
};

export default meta;
type Story = StoryObj<typeof EnvironmentsPage>;

export const Default: Story = {};

export const Empty: Story = {
  args: { rows: [] },
};

export const LoadError: Story = {
  args: {
    rows: [],
    connections: [],
    images: null,
    sizes: null,
    error: 'Failed to load environments: agents service unreachable (503).',
  },
};

export const Many: Story = {
  args: { rows: MANY_ENVIRONMENTS },
};

/** Search narrowed to environments that mention Slack. */
export const Filtered: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.type(await canvas.findByPlaceholderText('Search environments…'), 'slack');
  },
};

/** The create wizard on its first step. */
export const CreateWizardOpen: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(await canvas.findByRole('button', { name: /New environment/ }));
    await expect(
      await within(document.body).findByRole('dialog', { name: 'New environment' }),
    ).toBeTruthy();
  },
};

/** The edit wizard on its access step, with the effective access resolved from the environment's hosts and connectors. */
export const EditWizardOpen: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(await canvas.findByLabelText('Edit code-review'));
    const dialog = within(
      await within(document.body).findByRole('dialog', { name: 'Edit environment' }),
    );
    await userEvent.click(dialog.getByRole('button', { name: 'Continue' }));
    await userEvent.click(dialog.getByRole('button', { name: 'Continue' }));
    // The preview resolves after a short debounce.
    await dialog.findAllByText('api.github.com', {}, { timeout: 3000 });
  },
};

/** The delete confirmation. */
export const DeleteConfirmOpen: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(await canvas.findByLabelText('Delete incident-response'));
    await expect(await within(document.body).findByRole('dialog')).toBeTruthy();
  },
};
