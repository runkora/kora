import type { Meta, StoryObj } from '@storybook/react';
import { CopyButton } from './copy-button';

const meta: Meta<typeof CopyButton> = {
  title: 'App/Shared/CopyButton',
  component: CopyButton,
  tags: ['autodocs'],
};
export default meta;
type Story = StoryObj<typeof CopyButton>;

export const Default: Story = {
  args: { value: 'some-value-to-copy' },
};

export const CustomLabel: Story = {
  args: { value: 'sk-orbit-abc123', label: 'Copy API key' },
};
