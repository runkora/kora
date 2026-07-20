'use client';

import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Cell,
  ResponsiveContainer,
} from 'recharts';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@orbit/terra';
import { fmtUsd } from '@orbit/utils';
import { MODEL_COSTS } from '../../_data/mock';

// ── Chart Colors (Terra tokens as hex) ────────────────────────────────────────

const COLORS = {
  accent: '#e8a07a',
  healthy: '#9ce37d',
  degraded: '#ffd046',
  surfaceOverlay: '#34343d',
  borderSubtle: 'var(--color-border-subtle)',
  subtle: '#8a8a94',
};

const MODEL_BAR_COLORS = [
  COLORS.accent,
  COLORS.healthy,
  COLORS.degraded,
  COLORS.subtle,
  COLORS.borderSubtle,
];

// ── Helpers ───────────────────────────────────────────────────────────────────

// ── Component ─────────────────────────────────────────────────────────────────

export function CostByModelChart() {
  const data = [...MODEL_COSTS].sort((a, b) => b.cost - a.cost);

  return (
    <Card column>
      <CardHeader compact>
        <CardTitle>Cost by model</CardTitle>
        <CardDescription>Current billing period spend distribution</CardDescription>
      </CardHeader>
      <CardContent>
        <ResponsiveContainer width="100%" height={240}>
          <BarChart
            data={data}
            layout="vertical"
            margin={{ top: 4, right: 16, bottom: 0, left: 0 }}
          >
            <CartesianGrid strokeDasharray="3 3" stroke={COLORS.borderSubtle} horizontal={false} />
            <XAxis
              type="number"
              tick={{ fill: COLORS.subtle, fontSize: 11 }}
              tickLine={false}
              axisLine={false}
              tickFormatter={fmtUsd}
            />
            <YAxis
              type="category"
              dataKey="model"
              tick={{ fill: COLORS.subtle, fontSize: 11 }}
              tickLine={false}
              axisLine={false}
              width={110}
            />
            <Tooltip
              contentStyle={{
                backgroundColor: COLORS.surfaceOverlay,
                border: `1px solid ${COLORS.borderSubtle}`,
                borderRadius: '6px',
                padding: '8px 12px',
              }}
              formatter={(value: unknown) => [fmtUsd(Number(value)), 'Cost']}
              cursor={{ fill: 'color-mix(in oklch, var(--color-foreground) 5%, transparent)' }}
            />
            <Bar dataKey="cost" isAnimationActive={false}>
              {data.map((entry, index) => (
                <Cell
                  key={`cell-${entry.model}`}
                  fill={MODEL_BAR_COLORS[index % MODEL_BAR_COLORS.length]}
                />
              ))}
            </Bar>
          </BarChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  );
}
