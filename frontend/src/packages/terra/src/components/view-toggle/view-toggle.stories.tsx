import type { Meta, StoryObj } from '@storybook/react';
import { ViewToggle } from './view-toggle';

const meta: Meta<typeof ViewToggle> = {
  title: 'Terra/Molecules/ViewToggle',
  component: ViewToggle,
  tags: ['autodocs'],
};
export default meta;
type Story = StoryObj<typeof ViewToggle>;

export const Cards: Story = { args: { viewMode: 'cards', onToggle: () => {} } };
export const List: Story = { args: { viewMode: 'list', onToggle: () => {} } };
