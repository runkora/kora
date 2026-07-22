'use client';

import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Cell,
  ResponsiveContainer,
} from 'recharts';
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardDescription,
  Stack,
  Text,
} from '@orbit/terra';
import type { AnalyticsCostByModel } from '../../_types';

const COLORS = {
  subtle: 'var(--color-subtle)',
  borderSubtle: 'var(--color-border-subtle)',
  surfaceOverlay: 'var(--color-surface-overlay)',
};

const BAR_COLORS = [
  'var(--color-chart-1)',
  'var(--color-chart-2)',
  'var(--color-chart-3)',
  'var(--color-chart-4)',
  'var(--color-chart-5)',
  'var(--color-chart-6)',
  'var(--color-chart-7)',
  'var(--color-chart-8)',
];

function fmt(usd: number): string {
  return `$${usd.toFixed(2)}`;
}

interface ModelPoint {
  model: string;
  cost: number;
}

// Top 8 — one bar per chart token; never cycled.
function toPoints(costByModel: AnalyticsCostByModel[]): ModelPoint[] {
  return [...costByModel]
    .sort((a, b) => b.estimated_cost_usd - a.estimated_cost_usd)
    .slice(0, BAR_COLORS.length)
    .map((m) => ({ model: m.model_id, cost: m.estimated_cost_usd }));
}

export function AnalyticsCostByModelChart({
  costByModel,
}: {
  costByModel: AnalyticsCostByModel[];
}) {
  const data = toPoints(costByModel);

  if (data.length === 0) {
    return (
      <Card column>
        <CardHeader compact>
          <CardTitle>Cost by model</CardTitle>
          <CardDescription>Spend distribution across models</CardDescription>
        </CardHeader>
        <CardContent>
          <Stack align="center" justify="center" asChild>
            <figure className="h-[240px]">
              <Text size="supporting" color="subtle">
                No data for this period
              </Text>
            </figure>
          </Stack>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card column>
      <CardHeader compact>
        <CardTitle>Cost by model</CardTitle>
        <CardDescription>Spend distribution across models for selected period</CardDescription>
      </CardHeader>
      <CardContent>
        <ResponsiveContainer width="100%" height={240}>
          <BarChart
            data={data}
            layout="vertical"
            margin={{ top: 4, right: 16, bottom: 0, left: 0 }}
          >
            <CartesianGrid strokeDasharray="3 3" stroke={COLORS.borderSubtle} horizontal={false} />
            <XAxis
              type="number"
              tick={{ fill: COLORS.subtle, fontSize: 11 }}
              tickLine={false}
              axisLine={false}
              tickFormatter={fmt}
            />
            <YAxis
              type="category"
              dataKey="model"
              tick={{ fill: COLORS.subtle, fontSize: 11 }}
              tickLine={false}
              axisLine={false}
              width={110}
            />
            <Tooltip
              contentStyle={{
                backgroundColor: COLORS.surfaceOverlay,
                border: `1px solid ${COLORS.borderSubtle}`,
                borderRadius: '6px',
                padding: '8px 12px',
              }}
              formatter={(value: unknown) => [fmt(Number(value)), 'Cost']}
              cursor={{ fill: 'color-mix(in oklch, var(--color-foreground) 5%, transparent)' }}
            />
            <Bar dataKey="cost" isAnimationActive={false}>
              {data.map((entry, index) => (
                <Cell key={`cell-${entry.model}`} fill={BAR_COLORS[index]} />
              ))}
            </Bar>
          </BarChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  );
}
