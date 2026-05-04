import type { Meta, StoryObj } from '@storybook/react';
import { Progress } from './progress';
import { Text } from '../typography/typography';

const meta: Meta<typeof Progress> = {
  title: 'Terra/Molecules/Progress',
  component: Progress,
  parameters: {
    layout: 'centered',
    backgrounds: { default: 'orbit' },
  },
  tags: ['autodocs'],
  argTypes: {
    variant: {
      control: 'select',
      options: ['default', 'healthy', 'degraded', 'error', 'pending'],
    },
    size: {
      control: 'select',
      options: ['sm', 'md', 'lg'],
    },
    value: { control: { type: 'range', min: 0, max: 100 } },
    showValue: { control: 'boolean' },
  },
};

export default meta;
type Story = StoryObj<typeof Progress>;

export const Default: Story = {
  args: {
    label: 'CPU utilisation',
    value: 55,
    className: 'w-72',
  },
};

export const WithValue: Story = {
  args: {
    label: 'Memory usage',
    value: 72,
    showValue: true,
    className: 'w-72',
  },
};

export const Healthy: Story = {
  args: {
    label: 'Disk I/O',
    value: 38,
    variant: 'healthy',
    showValue: true,
    className: 'w-72',
  },
};

export const Degraded: Story = {
  args: {
    label: 'Network throughput',
    value: 78,
    variant: 'degraded',
    showValue: true,
    className: 'w-72',
  },
};

export const Error: Story = {
  args: {
    label: 'CPU utilisation',
    value: 96,
    variant: 'error',
    showValue: true,
    className: 'w-72',
  },
};

export const Pending: Story = {
  args: {
    label: 'Deployment progress',
    value: 45,
    variant: 'pending',
    showValue: true,
    className: 'w-72',
  },
};

export const AllSizes: Story = {
  render: () => (
    <div className="flex flex-col gap-4 w-72">
      <Progress label="Small" value={60} size="sm" showValue />
      <Progress label="Medium" value={60} size="md" showValue />
      <Progress label="Large" value={60} size="lg" showValue />
    </div>
  ),
};

export const AllVariants: Story = {
  render: () => (
    <div className="flex flex-col gap-3 w-72">
      <Progress label="Default" value={55} variant="default" showValue />
      <Progress label="Healthy" value={38} variant="healthy" showValue />
      <Progress label="Degraded" value={74} variant="degraded" showValue />
      <Progress label="Error" value={93} variant="error" showValue />
      <Progress label="Pending" value={45} variant="pending" showValue />
    </div>
  ),
};

export const ResourcePanel: Story = {
  render: () => (
    <div className="w-72 rounded-lg bg-surface p-4 flex flex-col gap-4">
      <Text size="micro" color="muted" weight="emphasis" transform="uppercase">
        Resource usage
      </Text>
      <div className="flex flex-col gap-3">
        <div className="flex flex-col gap-1.5">
          <Text size="micro" color="foreground">
            CPU
          </Text>
          <Progress label="CPU" value={42} variant="healthy" size="md" showValue />
        </div>
        <div className="flex flex-col gap-1.5">
          <Text size="micro" color="foreground">
            Memory
          </Text>
          <Progress label="Memory" value={71} variant="degraded" size="md" showValue />
        </div>
        <div className="flex flex-col gap-1.5">
          <Text size="micro" color="foreground">
            Disk
          </Text>
          <Progress label="Disk" value={94} variant="error" size="md" showValue />
        </div>
        <div className="flex flex-col gap-1.5">
          <Text size="micro" color="foreground">
            Network
          </Text>
          <Progress label="Network" value={28} variant="healthy" size="md" showValue />
        </div>
      </div>
    </div>
  ),
};

export const Empty: Story = {
  args: {
    label: 'No data yet',
    value: 0,
    showValue: true,
    className: 'w-72',
  },
};

export const Full: Story = {
  args: {
    label: 'Complete',
    value: 100,
    variant: 'healthy',
    showValue: true,
    className: 'w-72',
  },
};
