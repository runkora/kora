import type { Meta, StoryObj } from '@storybook/react';
import { Grid } from '../layout/grid';
import { Icon } from '../icon';
import { StatCard } from './stat-card';

const meta: Meta<typeof StatCard> = {
  title: 'Terra/Molecules/StatCard',
  component: StatCard,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof StatCard>;

/** Label, figure, supporting line, icon. */
export const Default: Story = {
  render: () => (
    <div className="w-[220px]">
      <StatCard
        label="Active triggers"
        value="40"
        detail="27 webhook · 11 scheduled"
        icon={<Icon name="activity" size="xs" />}
      />
    </div>
  ),
};

/** The figure alone, where there is nothing useful to add under it. */
export const ValueOnly: Story = {
  render: () => (
    <div className="w-[220px]">
      <StatCard label="Connected providers" value="3" />
    </div>
  ),
};

/**
 * `tone` colours the figure and the icon together, so a card reporting a
 * problem reads as one without the label being read.
 */
export const Tones: Story = {
  render: () => (
    <Grid cols={3} gap="snug" className="w-[680px]">
      <StatCard
        label="Success rate"
        value="98.9%"
        tone="healthy"
        detail="2,387 ok"
        icon={<Icon name="circle-check" size="xs" />}
      />
      <StatCard
        label="Failures (24h)"
        value="27"
        tone="error"
        detail="up from 4"
        icon={<Icon name="circle-x" size="xs" />}
      />
      <StatCard
        label="Queued"
        value="112"
        detail="drains in ~4m"
        icon={<Icon name="clock" size="xs" />}
      />
    </Grid>
  ),
};

/**
 * `detailTone` colours the supporting line alone, for when the caveat carries
 * the signal and the headline figure is neutral.
 */
export const DetailCarriesTheSignal: Story = {
  render: () => (
    <div className="w-[220px]">
      <StatCard
        label="Deliveries"
        value="2,414"
        detail="27 rejected by admission"
        detailTone="error"
        icon={<Icon name="send" size="xs" />}
      />
    </div>
  ),
};

/**
 * What the card is built for: a row of four or five, figures monospaced so they
 * align digit-for-digit and are comparable at a glance.
 */
export const Row: Story = {
  render: () => (
    <Grid cols={4} gap="snug" className="w-[900px]">
      <StatCard
        label="Virtual models"
        value="18"
        detail="14 enabled"
        icon={<Icon name="layers" size="xs" />}
      />
      <StatCard
        label="Provider models"
        value="126"
        detail="41 enabled"
        icon={<Icon name="circle-dot" size="xs" />}
      />
      <StatCard
        label="Connected providers"
        value="3"
        detail="discovered"
        icon={<Icon name="network" size="xs" />}
      />
      <StatCard
        label="Policy scopes"
        value="18"
        detail="auto-registered"
        icon={<Icon name="shield-check" size="xs" />}
      />
    </Grid>
  ),
};
