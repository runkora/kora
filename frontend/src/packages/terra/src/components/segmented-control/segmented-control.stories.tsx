import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Icon } from '../icon';
import { SegmentedControl } from './segmented-control';

const meta: Meta<typeof SegmentedControl> = {
  title: 'Terra/Molecules/SegmentedControl',
  component: SegmentedControl,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof SegmentedControl>;

function Controlled({
  options,
  initial,
  label,
}: {
  options: React.ComponentProps<typeof SegmentedControl>['options'];
  initial: string;
  label: string;
}) {
  const [value, setValue] = React.useState(initial);
  return (
    <SegmentedControl aria-label={label} options={options} value={value} onChange={setValue} />
  );
}

/** Filters partitioning one list, with the counts that make the split legible. */
export const Default: Story = {
  render: () => (
    <Controlled
      label="Filter by kind"
      initial="all"
      options={[
        { value: 'all', label: 'All', icon: <Icon name="layers" size="2xs" />, count: 40 },
        { value: 'webhook', label: 'Webhook', icon: <Icon name="webhook" size="2xs" />, count: 27 },
        {
          value: 'scheduled',
          label: 'Scheduled',
          icon: <Icon name="clock" size="2xs" />,
          count: 11,
        },
        { value: 'manual', label: 'Manual', icon: <Icon name="play" size="2xs" />, count: 2 },
      ]}
    />
  ),
};

/** Counts are optional. Without them the control is a plain filter switch. */
export const WithoutCounts: Story = {
  render: () => (
    <Controlled
      label="Filter by status"
      initial="active"
      options={[
        { value: 'active', label: 'Active' },
        { value: 'paused', label: 'Paused' },
        { value: 'archived', label: 'Archived' },
      ]}
    />
  ),
};

/** Icons alone, for a control narrow enough that the labels would crowd it. */
export const IconsOnly: Story = {
  render: () => (
    <Controlled
      label="Density"
      initial="list"
      options={[
        { value: 'list', label: '', icon: <Icon name="layout-list" size="2xs" /> },
        { value: 'grid', label: '', icon: <Icon name="layout-grid" size="2xs" /> },
      ]}
    />
  ),
};

/**
 * Two options: the smallest useful form. Below this, use a single toggle
 * button; above about six, the track stops being scannable and a select or
 * `Tabs` is the better surface.
 */
export const TwoOptions: Story = {
  render: () => (
    <Controlled
      label="Schedule mode"
      initial="simple"
      options={[
        { value: 'simple', label: 'Simple' },
        { value: 'cron', label: 'Cron expression' },
      ]}
    />
  ),
};
