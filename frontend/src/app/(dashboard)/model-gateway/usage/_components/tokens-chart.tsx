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
} from '@runkora/hades';
import { DAILY_TOKENS, type DailyTokenRow } from '../../_data/mock';
import { TableControls, type Granularity, type Period } from './table-controls';
import { aggregateByWeek, aggregateByMonth, filterByPeriod } from './utils/table-utils';

// ── Config ────────────────────────────────────────────────────────────────────

const CHART_CONFIG: ChartConfig = {
  totalTokens: { label: 'Total', color: 'var(--color-accent)' },
  promptTokens: { label: 'Prompt', color: 'var(--color-subtle)' },
  outputTokens: { label: 'Output', color: 'var(--color-muted)' },
};

// ── Helpers ───────────────────────────────────────────────────────────────────

interface TokenPoint {
  period: string;
  totalTokens: number;
  promptTokens: number;
  outputTokens: number;
}

function buildPoints(
  data: DailyTokenRow[],
  granularity: Granularity,
  period: Period,
): TokenPoint[] {
  const filtered = filterByPeriod(data, period);
  if (granularity === 'daily')
    return filtered.map((r) => ({
      period: r.date,
      totalTokens: r.totalTokens,
      promptTokens: r.promptTokens,
      outputTokens: r.outputTokens,
    }));
  if (granularity === 'weekly')
    return aggregateByWeek(filtered, (rows) => ({
      totalTokens: rows.reduce((s, r) => s + r.totalTokens, 0),
      promptTokens: rows.reduce((s, r) => s + r.promptTokens, 0),
      outputTokens: rows.reduce((s, r) => s + r.outputTokens, 0),
    }));
  return aggregateByMonth(filtered, (rows) => ({
    totalTokens: rows.reduce((s, r) => s + r.totalTokens, 0),
    promptTokens: rows.reduce((s, r) => s + r.promptTokens, 0),
    outputTokens: rows.reduce((s, r) => s + r.outputTokens, 0),
  }));
}

function fmtTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(0)}K`;
  return String(n);
}

// ── Component ─────────────────────────────────────────────────────────────────

export function TokensChart() {
  const [granularity, setGranularity] = useState<Granularity>('daily');
  const [period, setPeriod] = useState<Period>('30d');

  const data = buildPoints(DAILY_TOKENS, granularity, period);

  return (
    <Card column>
      <CardHeader compact>
        <Stack direction="horizontal" align="start" justify="between" gap="comfortable">
          <Stack gap="none">
            <CardTitle>Token usage over time</CardTitle>
            <CardDescription>Prompt and output token consumption</CardDescription>
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
              tickFormatter={fmtTokens}
              width={44}
            />
            <Tooltip
              cursor={{ stroke: 'var(--color-border-subtle)', strokeWidth: 1 }}
              content={<ChartTooltipContent formatter={(v) => fmtTokens(Number(v))} />}
            />
            <Legend content={<ChartLegendContent />} verticalAlign="bottom" />
            <Line
              type="monotone"
              dataKey="totalTokens"
              stroke="var(--color-totalTokens)"
              strokeWidth={2}
              dot={false}
              activeDot={{ r: 4 }}
            />
            <Line
              type="monotone"
              dataKey="promptTokens"
              stroke="var(--color-promptTokens)"
              strokeWidth={1.5}
              dot={false}
              activeDot={{ r: 4 }}
              strokeDasharray="4 2"
            />
            <Line
              type="monotone"
              dataKey="outputTokens"
              stroke="var(--color-outputTokens)"
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
