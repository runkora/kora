/** @tier 3 — catalog-scoped. Promote to src/components/ if a second feature needs this. */
'use client';

import { useEffect, useState } from 'react';
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, Legend } from 'recharts';
import {
  Alert,
  ChartContainer,
  ChartLegendContent,
  ChartTooltipContent,
  Stack,
  Text,
  type ChartConfig,
} from '@orbit/terra';
import { ApiRoute } from '@orbit/lib/routing';

// ── Types ─────────────────────────────────────────────────────────────────────

interface PriceHistoryPoint {
  date: string;
  input_cost_per_1m: number;
  output_cost_per_1m: number;
}

interface PriceHistoryChartProps {
  provider: string;
  modelId: string;
  days?: number;
}

// ── Chart config ──────────────────────────────────────────────────────────────

const CHART_CONFIG: ChartConfig = {
  input_cost_per_1m: { label: 'Input /1M', color: 'var(--color-accent)' },
  output_cost_per_1m: { label: 'Output /1M', color: 'var(--color-subtle)' },
};

function fmtPrice(v: number): string {
  return `$${v.toFixed(4)}`;
}

// ── Component ─────────────────────────────────────────────────────────────────

export function PriceHistoryChart({ provider, modelId, days = 90 }: PriceHistoryChartProps) {
  const [points, setPoints] = useState<PriceHistoryPoint[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    const load = async () => {
      setLoading(true);
      setError(null);
      try {
        const res = await fetch(ApiRoute.catalogPricingHistory.GET(provider, modelId, days));
        const body = (await res.json()) as { data?: PriceHistoryPoint[]; message?: string };
        if (!res.ok) throw new Error(body.message ?? `Request failed (${res.status})`);
        if (!cancelled) setPoints(body.data ?? []);
      } catch (err) {
        if (!cancelled)
          setError(err instanceof Error ? err.message : 'Failed to load price history.');
      } finally {
        if (!cancelled) setLoading(false);
      }
    };

    void load();

    return () => {
      cancelled = true;
    };
  }, [provider, modelId, days]);

  if (loading) {
    return (
      <Text size="caption" color="muted">
        Loading price history…
      </Text>
    );
  }

  if (error) {
    return <Alert variant="warning">{error}</Alert>;
  }

  if (points.length === 0) {
    return (
      <Text size="caption" color="muted">
        No price history — this model has no usage data yet.
      </Text>
    );
  }

  return (
    <Stack gap="snug">
      <Text size="supporting" weight="emphasis">
        Price history (last {days} days)
      </Text>
      <ChartContainer config={CHART_CONFIG} height={160}>
        <LineChart data={points} margin={{ top: 4, right: 8, bottom: 0, left: 4 }}>
          <CartesianGrid
            strokeDasharray="3 3"
            stroke="var(--color-border-subtle)"
            vertical={false}
          />
          <XAxis
            dataKey="date"
            tick={{ fill: 'var(--color-muted)', fontSize: 10 }}
            tickLine={false}
            axisLine={false}
            minTickGap={40}
          />
          <YAxis
            tick={{ fill: 'var(--color-muted)', fontSize: 10 }}
            tickLine={false}
            axisLine={false}
            tickFormatter={fmtPrice}
            width={48}
          />
          <Tooltip
            cursor={{ stroke: 'var(--color-border-subtle)', strokeWidth: 1 }}
            content={<ChartTooltipContent formatter={(v) => fmtPrice(Number(v))} />}
          />
          <Legend content={<ChartLegendContent />} verticalAlign="bottom" />
          <Line
            type="monotone"
            dataKey="input_cost_per_1m"
            stroke="var(--color-input_cost_per_1m)"
            strokeWidth={2}
            dot={false}
            activeDot={{ r: 3 }}
          />
          <Line
            type="monotone"
            dataKey="output_cost_per_1m"
            stroke="var(--color-output_cost_per_1m)"
            strokeWidth={1.5}
            dot={false}
            activeDot={{ r: 3 }}
            strokeDasharray="4 2"
          />
        </LineChart>
      </ChartContainer>
    </Stack>
  );
}
