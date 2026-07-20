import type { Meta, StoryObj } from '@storybook/react';
import { CopyField } from './copy-field';

const meta: Meta<typeof CopyField> = {
  title: 'App/Shared/CopyField',
  component: CopyField,
  tags: ['autodocs'],
  parameters: {
    layout: 'padded',
  },
};
export default meta;
type Story = StoryObj<typeof CopyField>;

export const Default: Story = {
  args: { value: 'some-value-to-copy' },
};

export const Mono: Story = {
  args: { value: 'sk-orbit-abc123def456', mono: true },
};

export const NoBorder: Story = {
  args: { value: 'sk-orbit-abc123def456', bordered: false },
};

export const NotFullWidth: Story = {
  args: { value: 'sk-orbit-abc123', fullWidth: false },
};
