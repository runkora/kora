import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, within } from 'storybook/test';
import { dashboardPage } from '@runkora/storybook/dashboard-frame';
import { ModelsTabShell } from './models-tab-shell';
import { MANY_MODEL_ROWS, MODEL_OBJECTS, MODEL_ROWS } from './model-mock-data';

/** The Models page as an operator sees it: favourites, then the full model table. */
const meta: Meta<typeof ModelsTabShell> = {
  title: 'Pages/Models',
  component: ModelsTabShell,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/model-gateway/models' } },
  },
  decorators: [
    dashboardPage({
      'GET /api/models': { object: 'list', data: MODEL_OBJECTS },
      'PUT /api/preferences': {},
    }),
  ],
};

export default meta;
type Story = StoryObj<typeof ModelsTabShell>;

/** Pins a few chat models so the favourites cards render above the table. */
const FAVOURITE_IDS = ['anthropic/claude-sonnet-4-6', 'anthropic/claude-haiku-4-5', 'azure/gpt-5'];

/** The table's default sort is by name, descending, so hearts follow that order. */
const pinFavourites: NonNullable<Story['play']> = async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const hearts = await canvas.findAllByRole('button', { name: 'Add to favourites' });
  const order = [...MODEL_ROWS].sort((a, b) => b.name.localeCompare(a.name)).map((m) => m.id);
  for (const id of FAVOURITE_IDS) {
    const heart = hearts[order.indexOf(id)];
    if (heart !== undefined) await userEvent.click(heart);
  }
};

export const Default: Story = {
  args: { initialModels: MODEL_ROWS, initialError: null },
  play: pinFavourites,
};

export const Empty: Story = {
  args: { initialModels: [], initialError: null },
};

export const Error: Story = {
  args: { initialModels: [], initialError: 'inference gateway returned 503' },
};

export const ManyModels: Story = {
  args: { initialModels: MANY_MODEL_ROWS, initialError: null },
};

/** Opens the detail modal for the first row. */
export const ModelDetail: Story = {
  args: { initialModels: MODEL_ROWS, initialError: null },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const buttons = await canvas.findAllByRole('button', { name: 'View model details' });
    const first = buttons[0];
    if (first !== undefined) await userEvent.click(first);
  },
};
