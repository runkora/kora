import type { Meta, StoryObj } from '@storybook/react';
import { expect, userEvent, within } from 'storybook/test';
import { dashboardPage } from '@orbit/storybook/dashboard-frame';
import { EnvironmentsPage } from './environments-page';
import { MANY_IMAGES_120 } from './sandbox-images-mock-data';
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

/** Opens the create wizard and returns it. */
async function openCreate(canvasElement: HTMLElement) {
  await userEvent.click(
    await within(canvasElement).findByRole('button', { name: /New environment/ }),
  );
  return within(await within(document.body).findByRole('dialog', { name: 'New environment' }));
}

/** Opens Custom on the Sandbox step and types the given CPU value. */
async function customSize(canvasElement: HTMLElement, cpu: string) {
  const dialog = await openCreate(canvasElement);
  await userEvent.click(dialog.getByRole('button', { name: 'Continue' }));
  await userEvent.click(dialog.getByRole('radio', { name: 'Custom' }));
  const input = dialog.getByLabelText(/^CPU/);
  await userEvent.clear(input);
  await userEvent.type(input, cpu);
}

/** The create wizard on its Start step, from the base image. */
export const CreateWizard: Story = {
  args: { images: MANY_IMAGES_120 },
  play: async ({ canvasElement }) => {
    await expect(await openCreate(canvasElement)).toBeTruthy();
  },
};

/** Copying code-review, renamed for a new team. */
export const CreateWizardCopy: Story = {
  args: { images: MANY_IMAGES_120 },
  play: async ({ canvasElement }) => {
    const dialog = await openCreate(canvasElement);
    await userEvent.click(dialog.getByRole('radio', { name: 'Copy an environment' }));
    await userEvent.click(dialog.getByRole('radio', { name: 'Code review' }));
    const name = dialog.getByLabelText(/^Name/);
    await userEvent.clear(name);
    await userEvent.type(name, 'Payments code review');
  },
};

/** The Sandbox step: the selected image, the image table and the size presets. */
export const CreateWizardSandbox: Story = {
  args: { images: MANY_IMAGES_120 },
  play: async ({ canvasElement }) => {
    const dialog = await openCreate(canvasElement);
    await userEvent.click(dialog.getByRole('button', { name: 'Continue' }));
  },
};

export const CreateWizardCustomSize: Story = {
  args: { images: MANY_IMAGES_120 },
  play: ({ canvasElement }) => customSize(canvasElement, '4'),
};

export const CreateWizardCustomSizeError: Story = {
  args: { images: MANY_IMAGES_120 },
  play: ({ canvasElement }) => customSize(canvasElement, '12'),
};

/** The Sandbox step when only kora-base is registered: a note replaces the image table. */
export const CreateWizardOnlyBaseImage: Story = {
  args: { images: MOCK_IMAGES.filter((i) => i.name === 'kora-base') },
  play: async ({ canvasElement }) => {
    const dialog = await openCreate(canvasElement);
    await userEvent.click(dialog.getByRole('button', { name: 'Continue' }));
  },
};

/** The edit wizard on its Identity step, with the slug locked. */
export const EditWizard: Story = {
  args: { images: MANY_IMAGES_120 },
  play: async ({ canvasElement }) => {
    await userEvent.click(await within(canvasElement).findByLabelText('Edit Code review'));
    await expect(
      await within(document.body).findByRole('dialog', { name: 'Edit environment' }),
    ).toBeTruthy();
  },
};

/** The edit wizard on its access step, with the effective access resolved from the environment's hosts and connectors. */
export const EditWizardAccess: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(await canvas.findByLabelText('Edit Code review'));
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
    await userEvent.click(await canvas.findByLabelText('Delete Incident response'));
    await expect(await within(document.body).findByRole('dialog')).toBeTruthy();
  },
};
