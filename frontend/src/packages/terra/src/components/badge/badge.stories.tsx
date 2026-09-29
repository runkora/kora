import type { Meta, StoryObj } from '@storybook/react';
import { Badge } from './badge';

const meta: Meta<typeof Badge> = {
  title: 'Terra/Atoms/Badge',
  component: Badge,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
  argTypes: {
    variant: {
      control: 'select',
      options: [
        'default',
        'secondary',
        'outline',
        'destructive',
        'healthy',
        'degraded',
        'error',
        'idle',
        'pending',
      ],
    },
    dot: { control: 'boolean' },
  },
};

export default meta;
type Story = StoryObj<typeof Badge>;

export const Default: Story = {
  args: { children: 'Default', variant: 'default' },
};

export const Secondary: Story = {
  args: { children: 'v1.4.2', variant: 'secondary' },
};

export const Outline: Story = {
  args: { children: 'Outline', variant: 'outline' },
};

export const Destructive: Story = {
  args: { children: 'Revoked', variant: 'destructive' },
};

export const Healthy: Story = {
  args: { children: 'Healthy', variant: 'healthy', dot: true },
};

export const Degraded: Story = {
  args: { children: 'Degraded', variant: 'degraded', dot: true },
};

export const Error: Story = {
  args: { children: 'Error', variant: 'error', dot: true },
};

export const Idle: Story = {
  args: { children: 'Idle', variant: 'idle', dot: true },
};

export const Pending: Story = {
  args: { children: 'Pending', variant: 'pending', dot: true },
};

/** All agent operational states at a glance */
export const StatusSignals: Story = {
  render: () => (
    <div className="flex flex-wrap gap-2 p-4">
      <Badge variant="healthy" dot>
        Healthy
      </Badge>
      <Badge variant="degraded" dot>
        Degraded
      </Badge>
      <Badge variant="error" dot>
        Error
      </Badge>
      <Badge variant="idle" dot>
        Idle
      </Badge>
      <Badge variant="pending" dot>
        Pending
      </Badge>
    </div>
  ),
};

/** Structural variants */
export const StructuralVariants: Story = {
  render: () => (
    <div className="flex flex-wrap gap-2 p-4">
      <Badge variant="default">Accent</Badge>
      <Badge variant="secondary">Secondary</Badge>
      <Badge variant="outline">Outline</Badge>
      <Badge variant="destructive">Destructive</Badge>
    </div>
  ),
};
