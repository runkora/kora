'use client';

import { useState } from 'react';
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
  Stack,
  type ChartConfig,
} from '@orbit/terra';
import { DAILY_SPEND, type DailySpendRow } from '../../_data/mock';
import { TableControls, type Granularity, type Period } from './table-controls';
import { aggregateByWeek, aggregateByMonth, filterByPeriod } from './utils/table-utils';

// ── Config ────────────────────────────────────────────────────────────────────

const CHART_CONFIG: ChartConfig = {
  totalUsd: { label: 'Total', color: 'var(--color-accent)' },
  promptUsd: { label: 'Prompt', color: 'var(--color-subtle)' },
  outputUsd: { label: 'Output', color: 'var(--color-muted)' },
};

// ── Helpers ───────────────────────────────────────────────────────────────────

interface SpendPoint {
  period: string;
  totalUsd: number;
  promptUsd: number;
  outputUsd: number;
}

function buildPoints(
  data: DailySpendRow[],
  granularity: Granularity,
  period: Period,
): SpendPoint[] {
  const filtered = filterByPeriod(data, period);
  if (granularity === 'daily')
    return filtered.map((r) => ({
      period: r.date,
      totalUsd: r.totalUsd,
      promptUsd: r.promptUsd,
      outputUsd: r.outputUsd,
    }));
  if (granularity === 'weekly')
    return aggregateByWeek(filtered, (rows) => ({
      totalUsd: rows.reduce((s, r) => s + r.totalUsd, 0),
      promptUsd: rows.reduce((s, r) => s + r.promptUsd, 0),
      outputUsd: rows.reduce((s, r) => s + r.outputUsd, 0),
    }));
  return aggregateByMonth(filtered, (rows) => ({
    totalUsd: rows.reduce((s, r) => s + r.totalUsd, 0),
    promptUsd: rows.reduce((s, r) => s + r.promptUsd, 0),
    outputUsd: rows.reduce((s, r) => s + r.outputUsd, 0),
  }));
}

function fmtUsd(n: number): string {
  return `$${n.toFixed(4)}`;
}

function fmtUsdAxis(n: number): string {
  return `$${n.toFixed(2)}`;
}

// ── Component ─────────────────────────────────────────────────────────────────

export function SpendChart() {
  const [granularity, setGranularity] = useState<Granularity>('daily');
  const [period, setPeriod] = useState<Period>('30d');

  const data = buildPoints(DAILY_SPEND, granularity, period);

  return (
    <Card column>
      <CardHeader compact>
        <Stack direction="horizontal" align="start" justify="between" gap="comfortable">
          <Stack gap="none">
            <CardTitle>Spend over time</CardTitle>
            <CardDescription>USD cost broken down by prompt and output tokens</CardDescription>
          </Stack>
          <TableControls
            granularity={granularity}
            period={period}
            onGranularityChange={setGranularity}
            onPeriodChange={setPeriod}
          />
        </Stack>
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
              dataKey="totalUsd"
              stroke="var(--color-totalUsd)"
              strokeWidth={2}
              dot={false}
              activeDot={{ r: 4 }}
            />
            <Line
              type="monotone"
              dataKey="promptUsd"
              stroke="var(--color-promptUsd)"
              strokeWidth={1.5}
              dot={false}
              activeDot={{ r: 4 }}
              strokeDasharray="4 2"
            />
            <Line
              type="monotone"
              dataKey="outputUsd"
              stroke="var(--color-outputUsd)"
              strokeWidth={1.5}
              dot={false}
              activeDot={{ r: 4 }}
              strokeDasharray="2 2"
            />
          </LineChart>
        </ChartContainer>
      </CardContent>
    </Card>
  );
}
