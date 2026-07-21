import type { Meta, StoryObj } from '@storybook/react';
import { CopyRow } from './copy-row';

const meta: Meta<typeof CopyRow> = {
  title: 'App/Shared/CopyRow',
  component: CopyRow,
  tags: ['autodocs'],
  parameters: {
    layout: 'padded',
  },
};
export default meta;
type Story = StoryObj<typeof CopyRow>;

export const Default: Story = {
  args: { label: 'Model ID', value: 'bedrock/claude-sonnet-4-5' },
};

export const Mono: Story = {
  args: { label: 'Request ID', value: 'req_01abc123def456xyz', mono: true },
};
