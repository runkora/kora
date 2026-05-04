import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '../layout/stack';
import { Text } from '../typography/typography';
import { StatusDot } from './status-dot';

const meta: Meta<typeof StatusDot> = {
  title: 'Terra/Atoms/StatusDot',
  component: StatusDot,
  parameters: {
    layout: 'centered',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#070a0f' }],
    },
  },
  tags: ['autodocs'],
  argTypes: {
    status: {
      control: 'select',
      options: ['healthy', 'degraded', 'error', 'idle', 'pending', 'muted', 'accent'],
    },
    size: {
      control: 'select',
      options: ['sm', 'md', 'lg'],
    },
    glow: { control: 'boolean' },
  },
};

export default meta;
type Story = StoryObj<typeof StatusDot>;

export const Default: Story = {
  args: { status: 'healthy', size: 'md' },
};

/** All seven status variants — healthy / degraded / error / idle / pending / muted / accent */
export const StatusVariants: Story = {
  render: () => (
    <Stack direction="horizontal" gap="comfortable" align="center" wrap>
      {(['healthy', 'degraded', 'error', 'idle', 'pending', 'muted', 'accent'] as const).map(
        (status) => (
          <Stack key={status} gap="tight" align="center">
            <StatusDot status={status} />
            <Text size="micro" color="muted">
              {status}
            </Text>
          </Stack>
        ),
      )}
    </Stack>
  ),
};

/** Three sizes: sm (6 px) / md (10 px) / lg (12 px) */
export const Sizes: Story = {
  render: () => (
    <Stack direction="horizontal" gap="comfortable" align="center">
      {(['sm', 'md', 'lg'] as const).map((size) => (
        <Stack key={size} gap="tight" align="center">
          <StatusDot status="healthy" size={size} />
          <Text size="micro" color="muted">
            {size}
          </Text>
        </Stack>
      ))}
    </Stack>
  ),
};

/** Glow ring reinforces "live / active" vs "inactive / error" at a glance */
export const WithGlow: Story = {
  render: () => (
    <Stack direction="horizontal" gap="comfortable" align="center">
      <StatusDot status="healthy" glow />
      <StatusDot status="degraded" glow />
      <StatusDot status="error" glow />
      <StatusDot status="pending" glow />
    </Stack>
  ),
};
