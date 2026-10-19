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
} from '@runkora/hades';
import type { AnalyticsBucket } from '../../_types';

const CHART_CONFIG: ChartConfig = {
  promptTokens: { label: 'Prompt', color: 'var(--color-accent)' },
  completionTokens: { label: 'Completion', color: 'var(--color-subtle)' },
  cacheReadTokens: { label: 'Cache read', color: 'var(--color-muted)' },
  cacheCreationTokens: { label: 'Cache write', color: 'var(--color-warning)' },
};

function fmtTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(0)}K`;
  return String(n);
}

interface TokenPoint {
  period: string;
  promptTokens: number;
  completionTokens: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
}

function toPoints(buckets: AnalyticsBucket[]): TokenPoint[] {
  return buckets.map((b) => ({
    period: b.date,
    promptTokens: b.prompt_tokens,
    completionTokens: b.completion_tokens,
    cacheReadTokens: b.cache_read_tokens,
    cacheCreationTokens: b.cache_creation_tokens,
  }));
}

export function AnalyticsTokensChart({ buckets }: { buckets: AnalyticsBucket[] }) {
  const data = toPoints(buckets);

  return (
    <Card column>
      <CardHeader compact>
        <CardTitle>Token usage over time</CardTitle>
        <CardDescription>Prompt, completion, and cache token consumption</CardDescription>
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
              dataKey="promptTokens"
              stroke="var(--color-promptTokens)"
              strokeWidth={2}
              dot={false}
              activeDot={{ r: 4 }}
              isAnimationActive={false}
            />
            <Line
              type="monotone"
              dataKey="completionTokens"
              stroke="var(--color-completionTokens)"
              strokeWidth={1.5}
              dot={false}
              activeDot={{ r: 4 }}
              strokeDasharray="4 2"
              isAnimationActive={false}
            />
            <Line
              type="monotone"
              dataKey="cacheReadTokens"
              stroke="var(--color-cacheReadTokens)"
              strokeWidth={1.5}
              dot={false}
              activeDot={{ r: 4 }}
              strokeDasharray="2 2"
              isAnimationActive={false}
            />
            <Line
              type="monotone"
              dataKey="cacheCreationTokens"
              stroke="var(--color-cacheCreationTokens)"
              strokeWidth={1.5}
              dot={false}
              activeDot={{ r: 4 }}
              strokeDasharray="2 4"
              isAnimationActive={false}
            />
          </LineChart>
        </ChartContainer>
      </CardContent>
    </Card>
  );
}
