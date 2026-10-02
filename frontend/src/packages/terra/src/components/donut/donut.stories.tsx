import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Text } from '../typography';
import { Donut, donutSegmentColor } from './donut';

/**
 * Donut
 *
 * A ring divided into proportional segments, with room in the middle for the total. Use it for a
 * few parts of one whole. Pair it with a legend that carries the exact numbers.
 */
const meta: Meta<typeof Donut> = {
  title: 'Terra/Atoms/Donut',
  component: Donut,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Donut>;

const ROLES = [
  { label: 'Custom', value: 3 },
  { label: 'Reserved', value: 1 },
];

const SCOPES = [
  { label: 'platform', value: 6 },
  { label: 'mcp', value: 5 },
  { label: 'agents', value: 4 },
  { label: 'inference', value: 2 },
  { label: 'triggers', value: 2 },
  { label: 'tokens', value: 1 },
];

function Centre({ children }: { children: React.ReactNode }) {
  return <span className="font-mono text-xl font-bold text-foreground">{children}</span>;
}

function Legend({ segments }: { segments: { label: string; value: number }[] }) {
  return (
    <ul className="space-y-1.5 text-[12px]">
      {segments.map((s, i) => (
        <li key={s.label} className="flex items-center gap-2">
          <span className="size-2 rounded-full" style={{ background: donutSegmentColor(i) }} />
          <span className="text-muted">{s.label}</span>
          <span className="ml-auto pl-4 font-medium tabular-nums text-foreground">{s.value}</span>
        </li>
      ))}
    </ul>
  );
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-md space-y-8">
      <Example title="With a legend">
        <div className="flex items-center gap-6">
          <Donut segments={SCOPES}>
            <Centre>20</Centre>
          </Donut>
          <Legend segments={SCOPES} />
        </div>
      </Example>
      <Text size="supporting" color="muted">
        Segments are one hue at falling strength, so their order carries no good or bad meaning. The
        legend reads its dot colours from donutSegmentColor.
      </Text>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option
          name="segments"
          use="The parts of the whole, largest first. Zero values draw nothing."
        >
          <Donut segments={ROLES}>
            <Centre>4</Centre>
          </Donut>
        </Option>
        <Option name="one part" use="A single part fills the ring, with no gap.">
          <Donut segments={[{ label: 'Enabled', value: 8 }]}>
            <Centre>8</Centre>
          </Donut>
        </Option>
        <Option name="no parts" use="An empty track, for a total of zero.">
          <Donut segments={[]}>
            <Centre>0</Centre>
          </Donut>
        </Option>
        <Option name="size, thickness" use="Outer diameter and ring width in pixels.">
          <Donut segments={ROLES} size={112} thickness={10}>
            <Centre>4</Centre>
          </Donut>
        </Option>
        <Option name="children" use="Content centred inside the ring, usually the total.">
          <Donut segments={ROLES}>
            <Text size="caption" color="muted">
              3 of 4
            </Text>
          </Donut>
        </Option>
      </OptionTable>
    </div>
  ),
};
