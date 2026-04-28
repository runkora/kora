import type { Meta, StoryObj } from '@storybook/react';
import { Button } from '@orbit/terra';
import { EmptyState } from './empty-state';

const meta: Meta<typeof EmptyState> = {
  title: 'App/Shared/EmptyState',
  component: EmptyState,
  tags: ['autodocs'],
};
export default meta;
type Story = StoryObj<typeof EmptyState>;

export const Default: Story = {
  args: {
    icon: 'shield-check',
    title: 'No grants yet',
    description: 'Grants assign scopes directly to principals. Create your first grant to define access.',
  },
};

export const WithAction: Story = {
  args: {
    icon: 'key',
    title: 'No API keys yet',
    description: 'Create a key to start making requests to the inference gateway.',
    action: <Button size="sm">Create your first key</Button>,
  },
};

export const SearchEmpty: Story = {
  args: {
    icon: 'search',
    title: 'No matches',
    description: 'No items match your current filters. Try adjusting your search.',
  },
};
