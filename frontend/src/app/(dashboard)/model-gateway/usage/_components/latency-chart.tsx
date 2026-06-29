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
  NativeSelect,
  Stack,
  type ChartConfig,
} from '@orbit/terra';
import { DAILY_LATENCY } from '../../_data/mock';
import { type Period } from './table-controls';
import { filterByPeriod } from './utils/table-utils';
import { fmtDateAxis } from '@orbit/utils';

// ── Config ────────────────────────────────────────────────────────────────────

const CHART_CONFIG: ChartConfig = {
  p50: { label: 'P50', color: 'var(--color-status-healthy)' },
  p95: { label: 'P95', color: 'var(--color-status-degraded)' },
  p99: { label: 'P99', color: 'var(--color-status-error)' },
};

// ── Chart Colors (Terra tokens as hex) ────────────────────────────────────────

const COLORS = {
  surfaceOverlay: '#34343d',
  borderSubtle: '#44444d',
  subtle: '#8a8a94',
};

// ── Helpers ───────────────────────────────────────────────────────────────────

function fmtLatency(ms: number): string {
  return `${Math.round(ms)}ms`;
}

function filterLatencyByPeriod(period: Period) {
  return filterByPeriod(DAILY_LATENCY, period);
}

// ── Period options ────────────────────────────────────────────────────────────

const PERIODS: { value: Period; label: string }[] = [
  { value: '7d', label: '7d' },
  { value: '14d', label: '14d' },
];

// ── Component ─────────────────────────────────────────────────────────────────

export function LatencyChart() {
  const [period, setPeriod] = useState<Period>('14d');

  const data = filterLatencyByPeriod(period);

  return (
    <Card column>
      <CardHeader compact>
        <Stack direction="horizontal" align="center" justify="between" gap="comfortable">
          <Stack gap="none">
            <CardTitle>Latency distribution</CardTitle>
            <CardDescription>P50, P95, and P99 request latency (daily)</CardDescription>
          </Stack>
          <NativeSelect
            value={period}
            fullWidth={false}
            aria-label="Period"
            onChange={(e) => setPeriod(e.target.value as Period)}
          >
            {PERIODS.map(({ value, label }) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </NativeSelect>
        </Stack>
      </CardHeader>
      <CardContent>
        <ChartContainer config={CHART_CONFIG} height={280}>
          <LineChart data={data} margin={{ top: 4, right: 16, bottom: 0, left: 8 }}>
            <CartesianGrid strokeDasharray="3 3" stroke={COLORS.borderSubtle} vertical={false} />
            <XAxis
              dataKey="date"
              tick={{ fill: COLORS.subtle, fontSize: 11 }}
              tickLine={false}
              axisLine={false}
              tickFormatter={fmtDateAxis}
              minTickGap={40}
            />
            <YAxis
              tick={{ fill: COLORS.subtle, fontSize: 11 }}
              tickLine={false}
              axisLine={false}
              tickFormatter={(n) => `${n}ms`}
              width={52}
            />
            <Tooltip
              cursor={{ stroke: COLORS.borderSubtle, strokeWidth: 1 }}
              content={
                <ChartTooltipContent
                  labelFormatter={(label: unknown) => fmtDateAxis(String(label))}
                  formatter={(v) => fmtLatency(Number(v))}
                />
              }
            />
            <Legend content={<ChartLegendContent />} verticalAlign="bottom" />
            <Line
              type="monotone"
              dataKey="p50"
              stroke="var(--color-p50)"
              strokeWidth={2}
              dot={false}
              activeDot={{ r: 4 }}
              isAnimationActive={false}
            />
            <Line
              type="monotone"
              dataKey="p95"
              stroke="var(--color-p95)"
              strokeWidth={2}
              dot={false}
              activeDot={{ r: 4 }}
              isAnimationActive={false}
            />
            <Line
              type="monotone"
              dataKey="p99"
              stroke="var(--color-p99)"
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
