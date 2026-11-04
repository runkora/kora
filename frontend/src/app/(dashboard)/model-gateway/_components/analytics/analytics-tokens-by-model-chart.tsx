'use client';

import { useMemo } from 'react';
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
import type { AnalyticsModelTokenBucket } from '../../_types';

// ── Palette ───────────────────────────────────────────────────────────────────

const PALETTE = [
  'var(--color-chart-1)',
  'var(--color-chart-2)',
  'var(--color-chart-3)',
  'var(--color-chart-4)',
  'var(--color-chart-5)',
  'var(--color-chart-6)',
  'var(--color-chart-7)',
  'var(--color-chart-8)',
];

const MAX_MODELS = 8;

// ── Helpers ───────────────────────────────────────────────────────────────────

function safeKey(modelId: string): string {
  return modelId.replace(/[^a-zA-Z0-9]/g, '_');
}

function fmtTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(0)}K`;
  return String(n);
}

function getTopModels(data: AnalyticsModelTokenBucket[], n: number): string[] {
  const totals = new Map<string, number>();
  data.forEach((b) => totals.set(b.model_id, (totals.get(b.model_id) ?? 0) + b.total_tokens));
  return [...totals.entries()]
    .sort((a, b) => b[1] - a[1])
    .slice(0, n)
    .map(([id]) => id);
}

type PivotRow = { [key: string]: string | number };

function pivotData(data: AnalyticsModelTokenBucket[], models: string[]): PivotRow[] {
  const modelSet = new Set(models);
  const byDate = new Map<string, Record<string, number>>();
  data.forEach((b) => {
    if (!modelSet.has(b.model_id)) return;
    const row = byDate.get(b.date) ?? {};
    row[safeKey(b.model_id)] = b.total_tokens;
    byDate.set(b.date, row);
  });
  return [...byDate.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([date, values]) => ({ period: date, ...values }));
}

// ── Component ─────────────────────────────────────────────────────────────────

export function AnalyticsTokensByModelChart({
  tokensByModel,
}: {
  tokensByModel: AnalyticsModelTokenBucket[];
}) {
  const topModels = useMemo(() => getTopModels(tokensByModel, MAX_MODELS), [tokensByModel]);

  const config = useMemo((): ChartConfig => {
    const c: ChartConfig = {};
    topModels.forEach((m, i) => {
      c[safeKey(m)] = { label: m, color: PALETTE[i % PALETTE.length] };
    });
    return c;
  }, [topModels]);

  const data = useMemo(() => pivotData(tokensByModel, topModels), [tokensByModel, topModels]);

  return (
    <Card column>
      <CardHeader compact>
        <CardTitle>Token usage by model</CardTitle>
        <CardDescription>Total tokens per model over time</CardDescription>
      </CardHeader>
      <CardContent>
        <ChartContainer config={config} height={280}>
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
            {topModels.map((model) => (
              <Line
                key={model}
                type="monotone"
                dataKey={safeKey(model)}
                stroke={`var(--color-${safeKey(model)})`}
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4 }}
                isAnimationActive={false}
              />
            ))}
          </LineChart>
        </ChartContainer>
      </CardContent>
    </Card>
  );
}
