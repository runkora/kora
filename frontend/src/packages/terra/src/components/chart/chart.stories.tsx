import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { RotateCcw } from 'lucide-react';
import {
  LineChart,
  Line,
  AreaChart,
  Area,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
} from 'recharts';
import { ChartContainer, ChartTooltipContent, ChartLegendContent } from './chart';
import type { ChartConfig } from './chart';

// ── Replay wrapper ─────────────────────────────────────────────────────────────
// Increments `epoch` on click, which remounts the keyed child and
// re-triggers recharts' entry animations from the start.

function WithReplay({ children }: { children: React.ReactNode }) {
  const [epoch, setEpoch] = React.useState(0);
  return (
    <div className="relative">
      <div key={epoch}>{children}</div>
      <button
        onClick={() => setEpoch((e) => e + 1)}
        title="Replay animation"
        className="absolute right-0 top-0 flex items-center gap-1 rounded px-1.5 py-1 text-[10px] text-muted transition-colors hover:bg-surface-elevated hover:text-foreground"
      >
        <RotateCcw size={11} />
        replay
      </button>
    </div>
  );
}

const meta: Meta = {
  title: 'Terra/Molecules/Chart',
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
  decorators: [
    (Story) => (
      <WithReplay>
        <Story />
      </WithReplay>
    ),
  ],
};

export default meta;
type Story = StoryObj;

// ── Shared data ───────────────────────────────────────────────────────────────

const spendData = [
  { day: 'Mon', total: 1.42, prompt: 0.91, completion: 0.51 },
  { day: 'Tue', total: 2.18, prompt: 1.34, completion: 0.84 },
  { day: 'Wed', total: 1.87, prompt: 1.12, completion: 0.75 },
  { day: 'Thu', total: 3.05, prompt: 1.88, completion: 1.17 },
  { day: 'Fri', total: 2.64, prompt: 1.61, completion: 1.03 },
  { day: 'Sat', total: 0.73, prompt: 0.44, completion: 0.29 },
  { day: 'Sun', total: 0.98, prompt: 0.59, completion: 0.39 },
];

const requestData = [
  { hour: '00:00', requests: 142 },
  { hour: '04:00', requests: 58 },
  { hour: '08:00', requests: 391 },
  { hour: '12:00', requests: 624 },
  { hour: '16:00', requests: 508 },
  { hour: '20:00', requests: 287 },
  { hour: '23:00', requests: 176 },
];

const agentData = [
  { agent: 'apollo-3', cost: 4.12 },
  { agent: 'hermes-1', cost: 1.87 },
  { agent: 'minerva-2', cost: 2.54 },
  { agent: 'daedalus-4', cost: 0.63 },
  { agent: 'icarus-5', cost: 3.29 },
];

// ── Stories ───────────────────────────────────────────────────────────────────

export const LineChartDefault: Story = {
  name: 'Line — spend over time',
  render: () => {
    const config: ChartConfig = {
      total: { label: 'Total', color: '#60a5fa' }, // blue-400
      prompt: { label: 'Prompt', color: '#94a3b8' }, // slate-400
    };
    return (
      <div className="w-[520px]">
        <p className="mb-3 text-xs font-medium uppercase tracking-wider text-muted">
          Daily spend — last 7 days
        </p>
        <ChartContainer config={config}>
          <LineChart data={spendData}>
            <CartesianGrid strokeDasharray="3 3" stroke="#1f2937" vertical={false} />
            <XAxis
              dataKey="day"
              tick={{ fill: '#6b7280', fontSize: 11 }}
              axisLine={false}
              tickLine={false}
            />
            <YAxis
              tickFormatter={(v) => `$${v}`}
              tick={{ fill: '#6b7280', fontSize: 11 }}
              axisLine={false}
              tickLine={false}
              width={36}
            />
            <Tooltip
              cursor={{ stroke: '#374151', strokeWidth: 1 }}
              content={<ChartTooltipContent formatter={(v) => `$${Number(v).toFixed(2)}`} />}
            />
            <Line dataKey="total" stroke="var(--color-total)" strokeWidth={2} dot={false} />
            <Line
              dataKey="prompt"
              stroke="var(--color-prompt)"
              strokeWidth={2}
              dot={false}
              strokeDasharray="4 2"
            />
          </LineChart>
        </ChartContainer>
      </div>
    );
  },
};

export const AreaChartDefault: Story = {
  name: 'Area — request volume',
  render: () => {
    const config: ChartConfig = {
      requests: { label: 'Requests', color: '#818cf8' }, // indigo-400
    };
    return (
      <div className="w-[520px]">
        <p className="mb-3 text-xs font-medium uppercase tracking-wider text-muted">
          Requests / hour
        </p>
        <ChartContainer config={config}>
          <AreaChart data={requestData}>
            <defs>
              <linearGradient id="requestGradient" x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor="var(--color-requests)" stopOpacity={0.25} />
                <stop offset="95%" stopColor="var(--color-requests)" stopOpacity={0} />
              </linearGradient>
            </defs>
            <CartesianGrid strokeDasharray="3 3" stroke="#1f2937" vertical={false} />
            <XAxis
              dataKey="hour"
              tick={{ fill: '#6b7280', fontSize: 11 }}
              axisLine={false}
              tickLine={false}
            />
            <YAxis
              tick={{ fill: '#6b7280', fontSize: 11 }}
              axisLine={false}
              tickLine={false}
              width={36}
            />
            <Tooltip
              cursor={{ stroke: '#374151', strokeWidth: 1 }}
              content={<ChartTooltipContent />}
            />
            <Area
              dataKey="requests"
              stroke="var(--color-requests)"
              strokeWidth={2}
              fill="url(#requestGradient)"
              dot={false}
            />
          </AreaChart>
        </ChartContainer>
      </div>
    );
  },
};

export const BarChartDefault: Story = {
  name: 'Bar — per-agent cost',
  render: () => {
    const config: ChartConfig = {
      cost: { label: 'Cost (USD)', color: '#34d399' }, // emerald-400
    };
    return (
      <div className="w-[520px]">
        <p className="mb-3 text-xs font-medium uppercase tracking-wider text-muted">
          Cost by agent — last 7 days
        </p>
        <ChartContainer config={config} height={220}>
          <BarChart data={agentData} barCategoryGap="35%">
            <CartesianGrid strokeDasharray="3 3" stroke="#1f2937" horizontal vertical={false} />
            <XAxis
              dataKey="agent"
              tick={{ fill: '#6b7280', fontSize: 11 }}
              axisLine={false}
              tickLine={false}
            />
            <YAxis
              tickFormatter={(v) => `$${v}`}
              tick={{ fill: '#6b7280', fontSize: 11 }}
              axisLine={false}
              tickLine={false}
              width={36}
            />
            <Tooltip
              cursor={{ fill: '#ffffff08' }}
              content={<ChartTooltipContent formatter={(v) => `$${Number(v).toFixed(2)}`} />}
            />
            <Bar dataKey="cost" fill="var(--color-cost)" radius={[3, 3, 0, 0]} />
          </BarChart>
        </ChartContainer>
      </div>
    );
  },
};

export const WithLegend: Story = {
  name: 'Multi-series with legend',
  render: () => {
    const config: ChartConfig = {
      total: { label: 'Total', color: '#60a5fa' },
      prompt: { label: 'Prompt', color: '#94a3b8' },
      completion: { label: 'Completion', color: '#f472b6' }, // pink-400
    };
    return (
      <div className="w-[520px]">
        <p className="mb-3 text-xs font-medium uppercase tracking-wider text-muted">
          Spend breakdown — last 7 days
        </p>
        <ChartContainer config={config} height={280}>
          <LineChart data={spendData}>
            <CartesianGrid strokeDasharray="3 3" stroke="#1f2937" vertical={false} />
            <XAxis
              dataKey="day"
              tick={{ fill: '#6b7280', fontSize: 11 }}
              axisLine={false}
              tickLine={false}
            />
            <YAxis
              tickFormatter={(v) => `$${v}`}
              tick={{ fill: '#6b7280', fontSize: 11 }}
              axisLine={false}
              tickLine={false}
              width={36}
            />
            <Tooltip
              cursor={{ stroke: '#374151', strokeWidth: 1 }}
              content={<ChartTooltipContent formatter={(v) => `$${Number(v).toFixed(2)}`} />}
            />
            <Legend content={<ChartLegendContent />} verticalAlign="bottom" />
            <Line dataKey="total" stroke="var(--color-total)" strokeWidth={2} dot={false} />
            <Line
              dataKey="prompt"
              stroke="var(--color-prompt)"
              strokeWidth={2}
              dot={false}
              strokeDasharray="4 2"
            />
            <Line
              dataKey="completion"
              stroke="var(--color-completion)"
              strokeWidth={2}
              dot={false}
              strokeDasharray="2 2"
            />
          </LineChart>
        </ChartContainer>
      </div>
    );
  },
};

export const Minimal: Story = {
  name: 'Minimal — no axes',
  render: () => {
    const config: ChartConfig = {
      total: { label: 'Spend', color: '#60a5fa' },
    };
    return (
      <div className="w-[320px]">
        <p className="mb-3 text-xs font-medium uppercase tracking-wider text-muted">Spend trend</p>
        <ChartContainer config={config} height={80}>
          <LineChart data={spendData}>
            <Tooltip
              cursor={{ stroke: '#374151', strokeWidth: 1 }}
              content={<ChartTooltipContent formatter={(v) => `$${Number(v).toFixed(2)}`} />}
            />
            <Line dataKey="total" stroke="var(--color-total)" strokeWidth={2} dot={false} />
          </LineChart>
        </ChartContainer>
      </div>
    );
  },
};
