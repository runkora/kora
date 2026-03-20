import type { Meta, StoryObj } from '@storybook/react';
import { Skeleton } from './skeleton';

const meta: Meta<typeof Skeleton> = {
  title: 'Terra/Atoms/Skeleton',
  component: Skeleton,
  parameters: {
    layout: 'centered',
    backgrounds: { default: 'orbit' },
  },
  tags: ['autodocs'],
  argTypes: {
    shape: {
      control: 'select',
      options: ['line', 'block', 'circle'],
      description: 'Border-radius and aspect behaviour',
    },
    width: {
      control: 'select',
      options: ['quarter', 'third', 'half', 'two-thirds', 'three-quarters', 'full'],
      description: 'Fraction of parent container width (ignored when shape="circle")',
    },
    size: {
      control: 'select',
      options: ['xs', 'sm', 'md', 'lg', 'xl'],
      description: 'Height token (also sets diameter when shape="circle")',
    },
  },
};

export default meta;
type Story = StoryObj<typeof Skeleton>;

// ─── Single-element stories ───────────────────────────────────────────────────

export const Default: Story = {
  args: {
    shape: 'line',
    width: 'half',
    size: 'sm',
  },
};

export const Circle: Story = {
  args: {
    shape: 'circle',
    size: 'lg',
  },
};

export const Block: Story = {
  args: {
    shape: 'block',
    width: 'full',
    size: 'xl',
  },
};

// ─── Composition stories ──────────────────────────────────────────────────────

export const Card: Story = {
  render: () => (
    <div className="w-72 rounded-lg bg-surface p-4 flex flex-col gap-3">
      <Skeleton shape="line" width="three-quarters" size="sm" />
      <Skeleton shape="line" width="full" size="xs" />
      <Skeleton shape="line" width="full" size="xs" />
      <Skeleton shape="line" width="two-thirds" size="xs" />
    </div>
  ),
};

export const AgentRow: Story = {
  render: () => (
    <div className="flex items-center gap-3 w-80">
      <Skeleton shape="circle" size="md" />
      <div className="flex flex-col gap-2 flex-1">
        <Skeleton shape="line" width="third" size="xs" />
        <Skeleton shape="line" width="half" size="xs" />
      </div>
      <Skeleton shape="block" width="quarter" size="xs" />
    </div>
  ),
};

export const TableRows: Story = {
  render: () => (
    <div className="flex flex-col gap-3 w-full max-w-lg">
      <div className="flex gap-4 pb-2 border-b border-border-subtle">
        <Skeleton shape="line" width="quarter" size="xs" />
        <Skeleton shape="line" width="quarter" size="xs" />
        <Skeleton shape="line" width="quarter" size="xs" />
        <Skeleton shape="line" width="quarter" size="xs" />
      </div>
      {Array.from({ length: 5 }).map((_, i) => (
        <div key={i} className="flex gap-4 py-1">
          <Skeleton shape="line" width="quarter" size="xs" />
          <Skeleton shape="line" width="quarter" size="xs" />
          <Skeleton shape="line" width="quarter" size="xs" />
          <Skeleton shape="line" width="quarter" size="xs" />
        </div>
      ))}
    </div>
  ),
};

export const MetricCard: Story = {
  render: () => (
    <div className="w-48 rounded-lg bg-surface p-4 flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <Skeleton shape="line" width="half" size="xs" />
        <Skeleton shape="circle" size="xs" />
      </div>
      <Skeleton shape="block" width="two-thirds" size="md" />
      <Skeleton shape="block" width="full" size="xs" />
    </div>
  ),
};

export const NavList: Story = {
  render: () => (
    <div className="flex flex-col gap-1 w-56 p-2">
      {Array.from({ length: 6 }).map((_, i) => (
        <div key={i} className="flex items-center gap-2.5 px-3 py-2">
          <Skeleton shape="circle" size="xs" />
          <Skeleton shape="line" width="half" size="xs" />
        </div>
      ))}
    </div>
  ),
};

export const ProfileHeader: Story = {
  render: () => (
    <div className="flex items-start gap-4 w-80">
      <Skeleton shape="circle" size="xl" />
      <div className="flex flex-col gap-2 flex-1 pt-1">
        <Skeleton shape="line" width="half" size="sm" />
        <Skeleton shape="line" width="three-quarters" size="xs" />
        <div className="flex gap-2 mt-1">
          <Skeleton shape="block" width="quarter" size="xs" />
          <Skeleton shape="block" width="third" size="xs" />
        </div>
      </div>
    </div>
  ),
};

export const AllSizes: Story = {
  render: () => (
    <div className="flex flex-col gap-3 w-64">
      <Skeleton shape="line" width="full" size="xs" />
      <Skeleton shape="line" width="full" size="sm" />
      <Skeleton shape="line" width="full" size="md" />
      <Skeleton shape="line" width="full" size="lg" />
      <Skeleton shape="line" width="full" size="xl" />
    </div>
  ),
};

export const AllWidths: Story = {
  render: () => (
    <div className="flex flex-col gap-3 w-64">
      <Skeleton shape="line" width="quarter" size="sm" />
      <Skeleton shape="line" width="third" size="sm" />
      <Skeleton shape="line" width="half" size="sm" />
      <Skeleton shape="line" width="two-thirds" size="sm" />
      <Skeleton shape="line" width="three-quarters" size="sm" />
      <Skeleton shape="line" width="full" size="sm" />
    </div>
  ),
};

export const AllCircleSizes: Story = {
  render: () => (
    <div className="flex items-end gap-4">
      <Skeleton shape="circle" size="xs" />
      <Skeleton shape="circle" size="sm" />
      <Skeleton shape="circle" size="md" />
      <Skeleton shape="circle" size="lg" />
      <Skeleton shape="circle" size="xl" />
    </div>
  ),
};
