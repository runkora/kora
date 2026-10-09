import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Text } from '../typography';
import { Sparkline } from './sparkline';

/**
 * Sparkline
 *
 * A row of small bars that shows a trend inside a table row or card. It has no axes,
 * labels or tooltip.
 */
const meta: Meta<typeof Sparkline> = {
  title: 'Hades/Atoms/Sparkline',
  component: Sparkline,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Sparkline>;

const BUSY = [3, 5, 2, 8, 6, 9, 4, 7, 5, 11, 6, 8];
const QUIET = [0, 1, 0, 0, 2, 0, 0, 1, 0, 0, 0, 1];
const BURSTS = [0, 0, 0, 4, 0, 0, 0, 0, 6, 0, 0, 0];

export const Overview: Story = {
  render: () => (
    <div className="max-w-md space-y-8">
      <Example title="In a list">
        <div className="space-y-3">
          <div className="flex items-center justify-between gap-4">
            <Text size="supporting">Nightly report</Text>
            <div className="w-36">
              <Sparkline values={BUSY} label="Nightly report, deliveries over 12 hours" />
            </div>
          </div>
          <div className="flex items-center justify-between gap-4">
            <Text size="supporting">Weekly digest</Text>
            <div className="w-36">
              <Sparkline values={QUIET} label="Weekly digest, deliveries over 12 hours" />
            </div>
          </div>
        </div>
      </Example>
      <Text size="supporting" color="muted">
        Each row is scaled to its own highest bar, so a quiet trigger stays readable next to a busy
        one.
      </Text>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name='tone="accent"' use="A live series. The default.">
          <div className="w-36">
            <Sparkline values={BUSY} tone="accent" label="Live series" />
          </div>
        </Option>
        <Option name='tone="muted"' use="A disabled item that should keep its place in the column.">
          <div className="w-36">
            <Sparkline values={BUSY} tone="muted" label="Disabled series" />
          </div>
        </Option>
        <Option name='tone="error"' use="A series that is failing.">
          <div className="w-36">
            <Sparkline values={BUSY} tone="error" label="Failing series" />
          </div>
        </Option>
        <Option name="height={16}" use="Height in pixels. Use a smaller value in dense rows.">
          <div className="w-36">
            <Sparkline values={BUSY} height={16} label="16 pixels tall" />
          </div>
        </Option>
        <Option name="height={40}" use="A taller plot for a card. The default is 24.">
          <div className="w-36">
            <Sparkline values={BUSY} height={40} label="40 pixels tall" />
          </div>
        </Option>
        <Option
          name="values with zeros"
          use="A zero still draws a short bar, so quiet time does not look like missing data."
        >
          <div className="w-36">
            <Sparkline values={BURSTS} label="Two bursts, otherwise idle" />
          </div>
        </Option>
        <Option
          name="label"
          use="Text read aloud by screen readers. Say what the bars count and over what time."
        >
          <Text size="supporting" mono color="muted">
            label=&quot;142 deliveries over 24 hours&quot;
          </Text>
        </Option>
      </OptionTable>
    </div>
  ),
};
