import type { Meta, StoryObj } from '@storybook/react';
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import { Example, Option, OptionTable } from '../../docs/story-ui';
import { ChartContainer, ChartLegendContent, ChartTooltipContent } from './chart';
import type { ChartConfig } from './chart';

/**
 * Chart
 *
 * Wraps a recharts chart so its colours, tooltip and legend follow the Hades theme. Series
 * colours come from the chart tokens.
 */
const meta: Meta<typeof ChartContainer> = {
  title: 'Hades/Molecules/Chart',
  component: ChartContainer,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof ChartContainer>;

const spend = [
  { day: 'Mon', total: 1.42, prompt: 0.91 },
  { day: 'Tue', total: 2.18, prompt: 1.34 },
  { day: 'Wed', total: 1.87, prompt: 1.12 },
  { day: 'Thu', total: 3.05, prompt: 1.88 },
  { day: 'Fri', total: 2.64, prompt: 1.61 },
  { day: 'Sat', total: 0.73, prompt: 0.44 },
  { day: 'Sun', total: 0.98, prompt: 0.59 },
];

const requests = [
  { hour: '00:00', requests: 142 },
  { hour: '04:00', requests: 58 },
  { hour: '08:00', requests: 391 },
  { hour: '12:00', requests: 624 },
  { hour: '16:00', requests: 508 },
  { hour: '20:00', requests: 287 },
];

const agentCost = [
  { agent: 'apollo-3', cost: 4.12 },
  { agent: 'hermes-1', cost: 1.87 },
  { agent: 'minerva-2', cost: 2.54 },
  { agent: 'icarus-5', cost: 3.29 },
];

const spendConfig: ChartConfig = {
  total: { label: 'Total', color: 'var(--color-chart-1)' },
  prompt: { label: 'Prompt', color: 'var(--color-chart-2)' },
};
const requestConfig: ChartConfig = {
  requests: { label: 'Requests', color: 'var(--color-chart-3)' },
};
const costConfig: ChartConfig = {
  cost: { label: 'Cost', color: 'var(--color-chart-5)' },
};

const tick = { fill: 'var(--color-subtle)', fontSize: 11 };
const dollars = (value: number | string) => `$${Number(value).toFixed(2)}`;

function GridLines() {
  return (
    <CartesianGrid stroke="var(--color-border-subtle)" strokeDasharray="3 3" vertical={false} />
  );
}

function SpendLines({ legend = false }: { legend?: boolean }) {
  return (
    <LineChart data={spend}>
      <GridLines />
      <XAxis dataKey="day" tick={tick} axisLine={false} tickLine={false} />
      <YAxis
        tickFormatter={(v) => `$${v}`}
        tick={tick}
        axisLine={false}
        tickLine={false}
        width={36}
      />
      <Tooltip content={<ChartTooltipContent formatter={dollars} />} />
      {legend && <Legend content={<ChartLegendContent />} verticalAlign="bottom" />}
      <Line
        dataKey="total"
        stroke="var(--color-total)"
        strokeWidth={2}
        dot={false}
        isAnimationActive={false}
      />
      <Line
        dataKey="prompt"
        stroke="var(--color-prompt)"
        strokeWidth={2}
        strokeDasharray="4 2"
        dot={false}
        isAnimationActive={false}
      />
    </LineChart>
  );
}

function RequestArea() {
  return (
    <AreaChart data={requests}>
      <GridLines />
      <XAxis dataKey="hour" tick={tick} axisLine={false} tickLine={false} />
      <YAxis tick={tick} axisLine={false} tickLine={false} width={36} />
      <Tooltip content={<ChartTooltipContent />} />
      <Area
        dataKey="requests"
        stroke="var(--color-requests)"
        fill="var(--color-requests)"
        fillOpacity={0.15}
        strokeWidth={2}
        isAnimationActive={false}
      />
    </AreaChart>
  );
}

function CostBars() {
  return (
    <BarChart data={agentCost} barCategoryGap="35%">
      <GridLines />
      <XAxis dataKey="agent" tick={tick} axisLine={false} tickLine={false} />
      <YAxis
        tickFormatter={(v) => `$${v}`}
        tick={tick}
        axisLine={false}
        tickLine={false}
        width={36}
      />
      <Tooltip content={<ChartTooltipContent formatter={dollars} />} />
      <Bar
        dataKey="cost"
        fill="var(--color-cost)"
        radius={[3, 3, 0, 0]}
        isAnimationActive={false}
      />
    </BarChart>
  );
}

export const Overview: Story = {
  render: () => (
    <div className="max-w-2xl space-y-8">
      <Example title="Daily spend for the last 7 days">
        <ChartContainer
          config={spendConfig}
          height={260}
          role="group"
          aria-label="Line chart of daily total and prompt spend over the last 7 days"
        >
          <SpendLines legend />
        </ChartContainer>
      </Example>
      <Example title="Cost by agent">
        <ChartContainer
          config={costConfig}
          height={220}
          role="group"
          aria-label="Bar chart of cost per agent over the last 7 days"
        >
          <CostBars />
        </ChartContainer>
      </Example>
    </div>
  ),
};

export const Options: Story = {
  render: () => (
    <div className="max-w-5xl">
      <OptionTable
        title="Chart types"
        note="ChartContainer takes any recharts chart as its only child. Hover a chart to see the tooltip."
      >
        <Option name="LineChart" use="Show how a value changes over time.">
          <ChartContainer config={spendConfig} height={160} role="group" aria-label="Line chart">
            <SpendLines />
          </ChartContainer>
        </Option>
        <Option name="AreaChart" use="Show a volume over time, such as requests per hour.">
          <ChartContainer
            config={requestConfig}
            height={160}
            role="group"
            aria-label="Area chart of requests per hour"
          >
            <RequestArea />
          </ChartContainer>
        </Option>
        <Option name="BarChart" use="Compare values across categories, such as agents.">
          <ChartContainer
            config={costConfig}
            height={160}
            role="group"
            aria-label="Bar chart of cost per agent"
          >
            <CostBars />
          </ChartContainer>
        </Option>
      </OptionTable>
      <OptionTable title="Container and parts">
        <Option
          name="config"
          use="Names each series and sets its colour. Reference it as var(--color-key)."
        >
          <ChartContainer
            config={spendConfig}
            height={160}
            role="group"
            aria-label="Line chart with two series"
          >
            <SpendLines />
          </ChartContainer>
        </Option>
        <Option name="height" use="Chart height in pixels. The width always fills the parent.">
          <ChartContainer
            config={costConfig}
            height={100}
            role="group"
            aria-label="Short bar chart of cost per agent"
          >
            <CostBars />
          </ChartContainer>
        </Option>
        <Option name="ChartLegendContent" use="A legend that uses the names from config.">
          <ChartContainer
            config={spendConfig}
            height={180}
            role="group"
            aria-label="Line chart with a legend"
          >
            <SpendLines legend />
          </ChartContainer>
        </Option>
        <Option
          name="ChartTooltipContent"
          use="The hover tooltip. Pass formatter to format values, such as dollars."
        >
          <ChartContainer
            config={costConfig}
            height={160}
            role="group"
            aria-label="Bar chart with a dollar tooltip"
          >
            <CostBars />
          </ChartContainer>
        </Option>
      </OptionTable>
    </div>
  ),
};
