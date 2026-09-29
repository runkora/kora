import type { Meta, StoryObj } from '@storybook/react';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { Icon } from '../icon';
import { Grid } from '../layout/grid';
import { StatCard } from './stat-card';

/**
 * StatCard
 *
 * One headline figure with a label and a supporting line. Use a row of them to show the health
 * of one system at a glance.
 */
const meta: Meta<typeof StatCard> = {
  title: 'Terra/Molecules/StatCard',
  component: StatCard,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof StatCard>;

export const Overview: Story = {
  render: () => (
    <div className="max-w-4xl">
      <Example title="A row of four">
        <Grid cols={4} gap="snug">
          <StatCard
            label="Active agents"
            value="18"
            detail="14 running"
            icon={<Icon name="activity" size="xs" />}
          />
          <StatCard
            label="Runs today"
            value="2,414"
            detail="up 12% on yesterday"
            icon={<Icon name="send" size="xs" />}
          />
          <StatCard
            label="Success rate"
            value="98.9%"
            tone="healthy"
            detail="2,387 ok"
            icon={<Icon name="circle-check" size="xs" />}
          />
          <StatCard
            label="Failures"
            value="27"
            tone="error"
            detail="up from 4"
            icon={<Icon name="circle-x" size="xs" />}
          />
        </Grid>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable title="Options">
        <Option name="label, value" use="The two required parts. The value uses a monospaced font.">
          <div className="w-56">
            <StatCard label="Connected providers" value="3" />
          </div>
        </Option>
        <Option name="detail" use="A line under the value for a comparison or a breakdown.">
          <div className="w-56">
            <StatCard label="Active triggers" value="40" detail="27 webhook · 11 scheduled" />
          </div>
        </Option>
        <Option name="icon" use="A small icon at the top right.">
          <div className="w-56">
            <StatCard
              label="Queued"
              value="112"
              detail="drains in about 4 minutes"
              icon={<Icon name="clock" size="xs" />}
            />
          </div>
        </Option>
        <Option
          name='tone="healthy"'
          use="Colours the value and icon green when the number is good news."
        >
          <div className="w-56">
            <StatCard
              label="Success rate"
              value="98.9%"
              tone="healthy"
              detail="2,387 ok"
              icon={<Icon name="circle-check" size="xs" />}
            />
          </div>
        </Option>
        <Option
          name='tone="error"'
          use="Colours the value and icon red when the number is a problem."
        >
          <div className="w-56">
            <StatCard
              label="Failures"
              value="27"
              tone="error"
              detail="up from 4"
              icon={<Icon name="circle-x" size="xs" />}
            />
          </div>
        </Option>
        <Option
          name='detailTone="error"'
          use="Colours only the detail line, when the warning is in the detail and not the value."
        >
          <div className="w-56">
            <StatCard
              label="Deliveries"
              value="2,414"
              detail="27 rejected"
              detailTone="error"
              icon={<Icon name="send" size="xs" />}
            />
          </div>
        </Option>
      </OptionTable>
    </div>
  ),
};
