import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '../layout/stack';
import { Text } from '../typography';
import { Sparkline } from './sparkline';

const meta: Meta<typeof Sparkline> = {
  title: 'Terra/Atoms/Sparkline',
  component: Sparkline,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof Sparkline>;

const BUSY = [3, 5, 2, 8, 6, 9, 4, 7, 5, 11, 6, 8];
const QUIET = [0, 1, 0, 0, 2, 0, 0, 1, 0, 0, 0, 1];

/** The shape is the whole message: no axes, no labels, no tooltip. */
export const Default: Story = {
  render: () => (
    <div className="w-[160px]">
      <Sparkline values={BUSY} label="Deliveries over 12 hours" />
    </div>
  ),
};

/**
 * Each series scales against its own peak, so a quiet row stays readable beside
 * a busy one instead of flattening to nothing.
 */
export const SelfScaling: Story = {
  render: () => (
    <Stack gap="snug" className="w-[220px]">
      <Stack direction="horizontal" gap="snug" align="center" justify="between">
        <Text size="caption" color="muted">
          busy
        </Text>
        <div className="w-[140px]">
          <Sparkline values={BUSY} label="Busy trigger" />
        </div>
      </Stack>
      <Stack direction="horizontal" gap="snug" align="center" justify="between">
        <Text size="caption" color="muted">
          quiet
        </Text>
        <div className="w-[140px]">
          <Sparkline values={QUIET} label="Quiet trigger" />
        </div>
      </Stack>
    </Stack>
  ),
};

/**
 * `muted` is the same data drawn as inactive: a disabled row keeps its shape
 * and its place in the column rather than going blank and breaking the scan.
 */
export const Tones: Story = {
  render: () => (
    <Stack gap="snug" className="w-[160px]">
      <Sparkline values={BUSY} tone="accent" label="Active" />
      <Sparkline values={BUSY} tone="muted" label="Disabled" />
      <Sparkline values={BUSY} tone="error" label="Failing" />
    </Stack>
  ),
};

/**
 * A zero bucket still draws a stub, so quiet periods read as quiet rather than
 * as missing data.
 */
export const ZeroFloor: Story = {
  render: () => (
    <div className="w-[160px]">
      <Sparkline values={[0, 0, 0, 4, 0, 0, 0, 0, 6, 0, 0, 0]} label="Two bursts, otherwise idle" />
    </div>
  ),
};

/** `height` sizes the plot area. 24px is the table-cell default. */
export const Heights: Story = {
  render: () => (
    <Stack gap="snug" className="w-[160px]">
      <Sparkline values={BUSY} height={16} label="16px" />
      <Sparkline values={BUSY} height={24} label="24px" />
      <Sparkline values={BUSY} height={40} label="40px" />
    </Stack>
  ),
};
