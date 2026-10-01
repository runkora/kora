import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '@orbit/terra';
import { BadgeList } from './badge-list';

const meta: Meta<typeof BadgeList> = {
  title: 'App/Shared/BadgeList',
  component: BadgeList,
  tags: ['autodocs'],
  decorators: [
    (Story) => (
      <Stack maxWidth="card">
        <Story />
      </Stack>
    ),
  ],
};
export default meta;
type Story = StoryObj<typeof BadgeList>;

export const Few: Story = { args: { values: ['chat', 'tools'] } };

/** More than fit in two rows: the rest sit behind "+N more". */
export const TwoRows: Story = {
  args: {
    maxRows: 2,
    noun: 'capabilities',
    values: ['chat', 'embeddings', 'vision', 'tools', 'reasoning', 'caching', 'batch', 'streaming'],
  },
};

export const Empty: Story = { args: { values: [] } };
