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
  p50LatencyMs: { label: 'P50', color: 'var(--color-status-healthy)' },
  p95LatencyMs: { label: 'P95', color: 'var(--color-status-degraded)' },
  p99LatencyMs: { label: 'P99', color: 'var(--color-status-error)' },
};

function fmtLatency(ms: number): string {
  return `${Math.round(ms)}ms`;
}

interface LatencyPoint {
  period: string;
  p50LatencyMs: number;
  p95LatencyMs: number;
  p99LatencyMs: number;
}

function toPoints(buckets: AnalyticsBucket[]): LatencyPoint[] {
  return buckets.map((b) => ({
    period: b.date,
    p50LatencyMs: b.p50_latency_ms,
    p95LatencyMs: b.p95_latency_ms,
    p99LatencyMs: b.p99_latency_ms,
  }));
}

export function AnalyticsLatencyChart({ buckets }: { buckets: AnalyticsBucket[] }) {
  const data = toPoints(buckets);

  return (
    <Card column>
      <CardHeader compact>
        <CardTitle>Latency distribution</CardTitle>
        <CardDescription>P50, P95, and P99 request latency per period bucket</CardDescription>
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
              tickFormatter={(n) => `${n}ms`}
              width={52}
            />
            <Tooltip
              cursor={{ stroke: 'var(--color-border-subtle)', strokeWidth: 1 }}
              content={<ChartTooltipContent formatter={(v) => fmtLatency(Number(v))} />}
            />
            <Legend content={<ChartLegendContent />} verticalAlign="bottom" />
            <Line
              type="monotone"
              dataKey="p50LatencyMs"
              stroke="var(--color-p50LatencyMs)"
              strokeWidth={2}
              dot={false}
              activeDot={{ r: 4 }}
              isAnimationActive={false}
            />
            <Line
              type="monotone"
              dataKey="p95LatencyMs"
              stroke="var(--color-p95LatencyMs)"
              strokeWidth={2}
              dot={false}
              activeDot={{ r: 4 }}
              isAnimationActive={false}
            />
            <Line
              type="monotone"
              dataKey="p99LatencyMs"
              stroke="var(--color-p99LatencyMs)"
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
