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
      <Example title="With a change">
        <Grid cols={2} gap="snug">
          <StatCard
            label="Total requests"
            value="4.8K"
            delta={{ text: '▲ 18.2% vs last 7 days', positive: true }}
            icon={<Icon name="activity" size="xs" />}
          />
          <StatCard
            label="Error rate"
            value="2.1%"
            delta={{ text: '▲ 0.4 points', positive: true, positiveIsBad: true }}
            icon={<Icon name="circle-x" size="xs" />}
          />
        </Grid>
      </Example>
      <Example title="With a breakdown and a link">
        <Grid cols={3} gap="snug">
          <StatCard
            label="Roles"
            value={4}
            breakdown={[
              { label: 'Custom', value: 3 },
              { label: 'Reserved', value: 1 },
            ]}
            footer={<a href="#roles">View all →</a>}
            icon={<Icon name="user-check" size="xs" />}
          />
          <StatCard
            label="Vocabulary"
            value={20}
            breakdown={[
              { label: 'platform', value: 6 },
              { label: 'mcp', value: 5 },
              { label: 'agents', value: 4 },
              { label: 'inference', value: 2 },
              { label: 'triggers', value: 2 },
              { label: 'tokens', value: 1 },
            ]}
            footer={<a href="#vocabulary">View all →</a>}
            icon={<Icon name="library" size="xs" />}
          />
          <StatCard
            label="Grants"
            value={7}
            breakdown={[
              { label: 'Agent', value: 3 },
              { label: 'Human', value: 2 },
              { label: 'PAT', value: 1 },
              { label: 'Service', value: 1 },
            ]}
            footer={<a href="#grants">View all →</a>}
            icon={<Icon name="shield-check" size="xs" />}
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
        <Option
          name="label, value"
          use="The two required parts. The label can be any casing and shows in capitals. The value takes a string or a number and uses a monospaced font."
        >
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
          name="delta"
          use="A change indicator under the value, green when positive and amber when not. Add positiveIsBad when a rise is bad news."
        >
          <div className="w-56">
            <StatCard
              label="Total requests"
              value="4.8K"
              delta={{ text: '▲ 18.2% vs last 7 days', positive: true }}
              icon={<Icon name="activity" size="xs" />}
            />
          </div>
        </Option>
        <Option
          name="breakdown, breakdownLimit"
          use="The parts of the figure, largest first. Draws a ring with the value in the centre and a legend that lists the first three parts, then +N more."
        >
          <div className="w-72">
            <StatCard
              label="Roles"
              value={5}
              breakdown={[
                { label: 'Custom', value: 3 },
                { label: 'Reserved', value: 2 },
              ]}
              icon={<Icon name="user-check" size="xs" />}
            />
          </div>
        </Option>
        <Option
          name="footer"
          use="A slot at the bottom right for a link, so the page supplies its own router link."
        >
          <div className="w-56">
            <StatCard
              label="Grants"
              value={42}
              footer={<a href="#grants">View all →</a>}
              icon={<Icon name="shield-check" size="xs" />}
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
