import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Icon } from '../icon';
import { Stack } from '../layout/stack';
import { Text } from '../typography';
import { IconTile } from './icon-tile';

/**
 * IconTile
 *
 * A small rounded box that holds an icon or an initial. Every tile of the same size takes the
 * same space, so a column of them lines up.
 */
const meta: Meta<typeof IconTile> = {
  title: 'Terra/Atoms/IconTile',
  component: IconTile,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof IconTile>;

const ROWS = [
  { icon: 'webhook', label: 'pr-review' },
  { icon: 'clock', label: 'nightly-audit' },
  { icon: 'play', label: 'incident-triage' },
] as const;

export const Overview: Story = {
  render: () => (
    <div className="max-w-xl space-y-8">
      <Example title="Leading marker on a list">
        <Stack gap="snug" className="w-72">
          {ROWS.map((row) => (
            <Stack key={row.label} direction="horizontal" gap="snug" align="center">
              <IconTile>
                <Icon name={row.icon} size="xs" />
              </IconTile>
              <Text size="supporting">{row.label}</Text>
            </Stack>
          ))}
        </Stack>
      </Example>
      <Example title="Initial instead of an icon">
        <Stack direction="horizontal" gap="snug" align="center">
          <IconTile>AP</IconTile>
          <Text size="supporting">apollo-3</Text>
        </Stack>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name='size="sm"' use="Small marker for a compact row.">
          <IconTile size="sm">
            <Icon name="clock" size="2xs" />
          </IconTile>
        </Option>
        <Option name='size="md"' use="The default size for a list row.">
          <IconTile size="md">
            <Icon name="clock" size="xs" />
          </IconTile>
        </Option>
        <Option name='size="lg"' use="Larger marker for the top of a panel.">
          <IconTile size="lg">
            <Icon name="clock" size="sm" />
          </IconTile>
        </Option>
        <Option name='tone="muted"' use="The resting look. The default.">
          <IconTile tone="muted">
            <Icon name="webhook" size="xs" />
          </IconTile>
        </Option>
        <Option name='tone="accent"' use="Highlights the row it belongs to.">
          <IconTile tone="accent">
            <Icon name="webhook" size="xs" />
          </IconTile>
        </Option>
      </OptionTable>
    </div>
  ),
};
