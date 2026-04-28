import type { Meta, StoryObj } from '@storybook/react';
import { Text } from '../typography/typography';
import { Callout } from './callout';

const meta: Meta<typeof Callout> = {
  title: 'Terra/Atoms/Callout',
  component: Callout,
  tags: ['autodocs'],
};
export default meta;
type Story = StoryObj<typeof Callout>;

export const Info: Story = {
  args: {
    variant: 'info',
    children: (
      <Text size="caption" color="muted">
        Grants assign specific scopes directly to a principal. Unlike roles, grants are explicit —
        they apply regardless of how the principal authenticates.
      </Text>
    ),
  },
};

export const Warning: Story = {
  args: {
    variant: 'warning',
    children: (
      <Text size="caption">
        Copy this key now — it will not be shown again after you close this dialog.
      </Text>
    ),
  },
};
