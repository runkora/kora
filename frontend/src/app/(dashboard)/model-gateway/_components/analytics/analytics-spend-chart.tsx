'use client';

import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, Legend } from 'recharts';
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardDescription,
  ChartContainer,
  ChartTooltipContent,
  ChartLegendContent,
  type ChartConfig,
} from '@clear-route/hades';
import type { AnalyticsBucket } from '../../_types';

const CHART_CONFIG: ChartConfig = {
  estimatedCostUsd: { label: 'Cost (USD)', color: 'var(--color-accent)' },
};

function fmtUsd(n: number): string {
  return `$${n.toFixed(4)}`;
}

function fmtUsdAxis(n: number): string {
  return `$${n.toFixed(2)}`;
}

interface SpendPoint {
  period: string;
  estimatedCostUsd: number;
}

function toPoints(buckets: AnalyticsBucket[]): SpendPoint[] {
  return buckets.map((b) => ({ period: b.date, estimatedCostUsd: b.estimated_cost_usd }));
}

export function AnalyticsSpendChart({ buckets }: { buckets: AnalyticsBucket[] }) {
  const data = toPoints(buckets);

  return (
    <Card column>
      <CardHeader compact>
        <CardTitle>Spend over time</CardTitle>
        <CardDescription>Estimated USD cost per period bucket</CardDescription>
      </CardHeader>
      <CardContent>
        <ChartContainer config={CHART_CONFIG} height={280}>
          <LineChart data={data} margin={{ top: 4, right: 16, bottom: 0, left: 8 }}>
            <CartesianGrid
              strokeDasharray="3 3"
              stroke="var(--color-border-subtle)"
              vertical={false}
            />
            <XAxis
              dataKey="period"
              tick={{ fill: 'var(--color-muted)', fontSize: 11 }}
              tickLine={false}
              axisLine={false}
              minTickGap={40}
            />
            <YAxis
              tick={{ fill: 'var(--color-muted)', fontSize: 11 }}
              tickLine={false}
              axisLine={false}
              tickFormatter={fmtUsdAxis}
              width={52}
            />
            <Tooltip
              cursor={{ stroke: 'var(--color-border-subtle)', strokeWidth: 1 }}
              content={<ChartTooltipContent formatter={(v) => fmtUsd(Number(v))} />}
            />
            <Legend content={<ChartLegendContent />} verticalAlign="bottom" />
            <Line
              type="monotone"
              dataKey="estimatedCostUsd"
              stroke="var(--color-estimatedCostUsd)"
              strokeWidth={2}
              dot={false}
              activeDot={{ r: 4 }}
              isAnimationActive={false}
            />
          </LineChart>
        </ChartContainer>
      </CardContent>
    </Card>
  );
}
