import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Icon } from '../icon/icon';
import { Text } from '../typography/typography';
import { SegmentedControl } from './segmented-control';
import type { SegmentedControlOption } from './segmented-control';

/**
 * SegmentedControl
 *
 * A row of filters that split one list, where only one is chosen at a time.
 * The chosen option is filled with the accent colour.
 */
const meta: Meta<typeof SegmentedControl> = {
  title: 'Hades/Molecules/SegmentedControl',
  component: SegmentedControl,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof SegmentedControl>;

function Controlled({
  options,
  initial,
  label,
}: {
  options: SegmentedControlOption[];
  initial: string;
  label: string;
}) {
  const [value, setValue] = React.useState(initial);
  return (
    <SegmentedControl aria-label={label} options={options} value={value} onChange={setValue} />
  );
}

const RUN_FILTERS: SegmentedControlOption[] = [
  { value: 'all', label: 'All', icon: <Icon name="layers" size="2xs" />, count: 40 },
  { value: 'webhook', label: 'Webhook', icon: <Icon name="webhook" size="2xs" />, count: 27 },
  { value: 'scheduled', label: 'Scheduled', icon: <Icon name="clock" size="2xs" />, count: 11 },
  { value: 'manual', label: 'Manual', icon: <Icon name="play" size="2xs" />, count: 2 },
];

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl space-y-8">
      <Example title="Run filter with counts">
        <Controlled label="Filter runs by trigger" initial="webhook" options={RUN_FILTERS} />
        <Text size="supporting" color="muted">
          The numbers show how many runs each filter would list.
        </Text>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable
        title="Options"
        note="Every sample is live. The chosen option keeps the same width as the others."
      >
        <Option name="label only" use="The plainest form: text options.">
          <Controlled
            label="Filter by status"
            initial="active"
            options={[
              { value: 'active', label: 'Active' },
              { value: 'paused', label: 'Paused' },
              { value: 'archived', label: 'Archived' },
            ]}
          />
        </Option>
        <Option name="count" use="Show how many items each option would list.">
          <Controlled
            label="Filter by outcome"
            initial="failed"
            options={[
              { value: 'all', label: 'All', count: 120 },
              { value: 'failed', label: 'Failed', count: 8 },
              { value: 'passed', label: 'Passed', count: 112 },
            ]}
          />
        </Option>
        <Option name="icon" use="Put a small icon before the label.">
          <Controlled
            label="Filter by source"
            initial="webhook"
            options={[
              { value: 'webhook', label: 'Webhook', icon: <Icon name="webhook" size="2xs" /> },
              { value: 'scheduled', label: 'Scheduled', icon: <Icon name="clock" size="2xs" /> },
            ]}
          />
        </Option>
        <Option
          name="two options"
          use="The smallest useful set. For one choice, use a toggle button."
        >
          <Controlled
            label="Schedule mode"
            initial="simple"
            options={[
              { value: 'simple', label: 'Simple' },
              { value: 'cron', label: 'Cron expression' },
            ]}
          />
        </Option>
      </OptionTable>
    </div>
  ),
};
