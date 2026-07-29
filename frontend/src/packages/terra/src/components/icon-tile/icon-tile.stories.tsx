import type { Meta, StoryObj } from '@storybook/react';
import { Icon } from '../icon';
import { Stack } from '../layout/stack';
import { Text } from '../typography';
import { IconTile } from './icon-tile';

const meta: Meta<typeof IconTile> = {
  title: 'Terra/Atoms/IconTile',
  component: IconTile,
  parameters: {
    layout: 'centered',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#060709' }],
    },
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof IconTile>;

/** The resting tile: a fixed footprint for whatever glyph sits inside. */
export const Default: Story = {
  render: () => (
    <IconTile>
      <Icon name="clock" size="xs" />
    </IconTile>
  ),
};

/** Three footprints: a row's leading glyph, the default, and a header identity tile. */
export const Sizes: Story = {
  render: () => (
    <Stack direction="horizontal" gap="snug" align="center">
      <IconTile size="sm">
        <Icon name="clock" size="2xs" />
      </IconTile>
      <IconTile size="md">
        <Icon name="clock" size="xs" />
      </IconTile>
      <IconTile size="lg">
        <Icon name="clock" size="sm" />
      </IconTile>
    </Stack>
  ),
};

/**
 * `accent` marks a highlighted row, `muted` is the resting state. Both are the
 * same surface family, so a highlighted row reads as emphasised rather than as
 * a different kind of thing.
 */
export const Tones: Story = {
  render: () => (
    <Stack direction="horizontal" gap="snug" align="center">
      <IconTile tone="muted">
        <Icon name="webhook" size="xs" />
      </IconTile>
      <IconTile tone="accent">
        <Icon name="webhook" size="xs" />
      </IconTile>
    </Stack>
  ),
};

/** An initial instead of an icon, for a compact identity chip. */
export const WithInitial: Story = {
  render: () => (
    <Stack direction="horizontal" gap="snug" align="center">
      <IconTile size="sm">A</IconTile>
      <IconTile>OB</IconTile>
    </Stack>
  ),
};

/** What it is for: a column of rows lining up whatever the glyph inside. */
export const InAList: Story = {
  render: () => (
    <Stack gap="snug" className="w-[280px]">
      {[
        { icon: 'webhook', label: 'pr-review' },
        { icon: 'clock', label: 'nightly-audit' },
        { icon: 'play', label: 'incident-triage' },
      ].map((row) => (
        <Stack key={row.label} direction="horizontal" gap="snug" align="center">
          <IconTile>
            <Icon name={row.icon as 'webhook'} size="xs" />
          </IconTile>
          <Text size="caption">{row.label}</Text>
        </Stack>
      ))}
    </Stack>
  ),
};
