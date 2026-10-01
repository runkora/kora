import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '../layout/stack';
import { TruncatingBadge } from './truncating-badge';

const meta: Meta<typeof TruncatingBadge> = {
  title: 'Terra/Molecules/TruncatingBadge',
  component: TruncatingBadge,
  parameters: {
    layout: 'centered',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#060709' }],
    },
  },
  tags: ['autodocs'],
  args: { label: 'Kubernetes', variant: 'secondary', textColor: 'muted' },
};
export default meta;
type Story = StoryObj<typeof TruncatingBadge>;

/** Natural width when the container has room. */
export const Fits: Story = {};

/** Ends in an ellipsis, with the full label as the tooltip, when squeezed. */
export const Truncates: Story = {
  args: {
    label: 'Automated Deployment Safety Reviewer',
    variant: 'outline',
    textColor: 'foreground',
  },
  decorators: [
    (Story) => (
      <Stack direction="horizontal" maxWidth="card">
        <Story />
      </Stack>
    ),
  ],
};
