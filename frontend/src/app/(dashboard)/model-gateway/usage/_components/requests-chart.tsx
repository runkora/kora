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
import { DAILY_REQUESTS, type DailyRequestRow } from '../../_data/mock';
import { TableControls, type Granularity, type Period } from './table-controls';
import { aggregateByWeek, aggregateByMonth, filterByPeriod } from './utils/table-utils';

// ── Config ────────────────────────────────────────────────────────────────────

const CHART_CONFIG: ChartConfig = {
  total: { label: 'Total', color: 'var(--color-accent)' },
  successful: { label: 'Successful', color: 'var(--color-status-healthy)' },
  errored: { label: 'Errored', color: 'var(--color-status-error)' },
};

// ── Helpers ───────────────────────────────────────────────────────────────────

interface RequestPoint {
  period: string;
  total: number;
  successful: number;
  errored: number;
}

function buildPoints(
  data: DailyRequestRow[],
  granularity: Granularity,
  period: Period,
): RequestPoint[] {
  const filtered = filterByPeriod(data, period);
  if (granularity === 'daily')
    return filtered.map((r) => ({
      period: r.date,
      total: r.total,
      successful: r.successful,
      errored: r.errored,
    }));
  if (granularity === 'weekly')
    return aggregateByWeek(filtered, (rows) => ({
      total: rows.reduce((s, r) => s + r.total, 0),
      successful: rows.reduce((s, r) => s + r.successful, 0),
      errored: rows.reduce((s, r) => s + r.errored, 0),
    }));
  return aggregateByMonth(filtered, (rows) => ({
    total: rows.reduce((s, r) => s + r.total, 0),
    successful: rows.reduce((s, r) => s + r.successful, 0),
    errored: rows.reduce((s, r) => s + r.errored, 0),
  }));
}

function fmtCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(0)}K`;
  return String(n);
}

// ── Component ─────────────────────────────────────────────────────────────────

export function RequestsChart() {
  const [granularity, setGranularity] = useState<Granularity>('weekly');
  const [period, setPeriod] = useState<Period>('30d');

  const data = buildPoints(DAILY_REQUESTS, granularity, period);

  return (
    <Card column>
      <CardHeader compact>
        <Stack direction="horizontal" align="start" justify="between" gap="comfortable">
          <Stack gap="none">
            <CardTitle>Requests over time</CardTitle>
            <CardDescription>Total, successful, and errored requests</CardDescription>
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
              tickFormatter={fmtCount}
              width={44}
            />
            <Tooltip
              cursor={{ stroke: 'var(--color-border-subtle)', strokeWidth: 1 }}
              content={<ChartTooltipContent formatter={(v) => fmtCount(Number(v))} />}
            />
            <Legend content={<ChartLegendContent />} verticalAlign="bottom" />
            <Line
              type="monotone"
              dataKey="total"
              stroke="var(--color-total)"
              strokeWidth={2}
              dot={false}
              activeDot={{ r: 4 }}
            />
            <Line
              type="monotone"
              dataKey="successful"
              stroke="var(--color-successful)"
              strokeWidth={1.5}
              dot={false}
              activeDot={{ r: 4 }}
            />
            <Line
              type="monotone"
              dataKey="errored"
              stroke="var(--color-errored)"
              strokeWidth={1.5}
              dot={false}
              activeDot={{ r: 4 }}
              strokeDasharray="3 2"
            />
          </LineChart>
        </ChartContainer>
      </CardContent>
    </Card>
  );
}
