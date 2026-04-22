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
} from '@orbit/terra';
import type { AnalyticsBucket } from '../../_types';

const CHART_CONFIG: ChartConfig = {
  requests: { label: 'Total', color: 'var(--color-accent)' },
  successfulRequests: { label: 'Successful', color: 'var(--color-status-healthy)' },
  failedRequests: { label: 'Failed', color: 'var(--color-status-error)' },
};

function fmtCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(0)}K`;
  return String(n);
}

interface RequestPoint {
  period: string;
  requests: number;
  successfulRequests: number;
  failedRequests: number;
}

function toPoints(buckets: AnalyticsBucket[]): RequestPoint[] {
  return buckets.map((b) => ({
    period: b.date,
    requests: b.requests,
    successfulRequests: b.successful_requests,
    failedRequests: b.failed_requests,
  }));
}

export function AnalyticsRequestsChart({ buckets }: { buckets: AnalyticsBucket[] }) {
  const data = toPoints(buckets);

  return (
    <Card column>
      <CardHeader compact>
        <CardTitle>Requests over time</CardTitle>
        <CardDescription>Total, successful, and failed requests per period bucket</CardDescription>
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
              dataKey="requests"
              stroke="var(--color-requests)"
              strokeWidth={2}
              dot={false}
              activeDot={{ r: 4 }}
              isAnimationActive={false}
            />
            <Line
              type="monotone"
              dataKey="successfulRequests"
              stroke="var(--color-successfulRequests)"
              strokeWidth={1.5}
              dot={false}
              activeDot={{ r: 4 }}
              isAnimationActive={false}
            />
            <Line
              type="monotone"
              dataKey="failedRequests"
              stroke="var(--color-failedRequests)"
              strokeWidth={1.5}
              dot={false}
              activeDot={{ r: 4 }}
              strokeDasharray="3 2"
              isAnimationActive={false}
            />
          </LineChart>
        </ChartContainer>
      </CardContent>
    </Card>
  );
}
