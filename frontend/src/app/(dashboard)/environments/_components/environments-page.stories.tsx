import type { Meta, StoryObj } from '@storybook/react';
import { expect, userEvent, within } from 'storybook/test';
import { dashboardPage } from '@orbit/storybook/dashboard-frame';
import EnvironmentsLayout from '../layout';
import { EnvironmentsPage } from './environments-page';
import {
  MANY_ENVIRONMENTS,
  MOCK_CATALOG,
  MOCK_ENVIRONMENTS,
  MOCK_IMAGES,
  MOCK_SIZES,
  previewHandler,
} from './environments-mock-data';

/** The environments list and its create/edit modal, with mock environments and a mock preview endpoint. */
const meta: Meta<typeof EnvironmentsPage> = {
  title: 'Pages/Environments',
  component: EnvironmentsPage,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/environments' } },
  },
  decorators: [
    dashboardPage(
      {
        'POST /api/agents/types/effective-access': previewHandler,
        'DELETE /api/agents/environments/:id': { ok: true },
      },
      EnvironmentsLayout,
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof EnvironmentsPage>;

export const Default: Story = {
  args: {
    rows: MOCK_ENVIRONMENTS,
    connections: MOCK_CATALOG,
    images: MOCK_IMAGES,
    sizes: MOCK_SIZES,
    error: null,
  },
};

export const Empty: Story = {
  args: {
    rows: [],
    connections: MOCK_CATALOG,
    images: MOCK_IMAGES,
    sizes: MOCK_SIZES,
    error: null,
  },
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
  args: {
    rows: MANY_ENVIRONMENTS,
    connections: MOCK_CATALOG,
    images: MOCK_IMAGES,
    sizes: MOCK_SIZES,
    error: null,
  },
};

/** Search narrowed to environments that mention Slack. */
export const Filtered: Story = {
  args: {
    rows: MOCK_ENVIRONMENTS,
    connections: MOCK_CATALOG,
    images: MOCK_IMAGES,
    sizes: MOCK_SIZES,
    error: null,
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.type(await canvas.findByPlaceholderText('Search environments…'), 'slack');
  },
};

/** The create modal, empty. */
export const CreateFormOpen: Story = {
  args: {
    rows: MOCK_ENVIRONMENTS,
    connections: MOCK_CATALOG,
    images: MOCK_IMAGES,
    sizes: MOCK_SIZES,
    error: null,
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(await canvas.findByRole('button', { name: /New environment/ }));
    await expect(await within(document.body).findByRole('dialog')).toBeTruthy();
  },
};

/** The edit modal scrolled to the effective access panel, resolved from the environment's hosts and connectors. */
export const EditFormOpen: Story = {
  args: {
    rows: MOCK_ENVIRONMENTS,
    connections: MOCK_CATALOG,
    images: MOCK_IMAGES,
    sizes: MOCK_SIZES,
    error: null,
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(await canvas.findByLabelText('Edit code-review'));
    const body = within(document.body);
    await expect(await body.findByRole('heading', { name: 'Edit environment' })).toBeTruthy();
    // The preview resolves after a short debounce; wait for a resolved host, then scroll the modal body to the bottom.
    const dialog = await body.findByRole('dialog');
    await within(dialog).findAllByText('api.github.com', {}, { timeout: 3000 });
    const scroller = [...dialog.querySelectorAll<HTMLElement>('*')].find(
      (el) => getComputedStyle(el).overflowY === 'auto' && el.scrollHeight > el.clientHeight,
    );
    if (scroller) scroller.scrollTop = scroller.scrollHeight;
  },
};

/** The delete confirmation. */
export const DeleteConfirmOpen: Story = {
  args: {
    rows: MOCK_ENVIRONMENTS,
    connections: MOCK_CATALOG,
    images: MOCK_IMAGES,
    sizes: MOCK_SIZES,
    error: null,
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(await canvas.findByLabelText('Delete incident-response'));
    await expect(await within(document.body).findByRole('dialog')).toBeTruthy();
  },
};
