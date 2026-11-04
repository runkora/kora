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
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@runkora/hades';
import { fmtUsd } from '@runkora/utils';
import { MODEL_COSTS } from '../../_data/mock';

// ── Chart Colors (Hades tokens) ───────────────────────────────────────────────

const COLORS = {
  surfaceOverlay: 'var(--color-surface-overlay)',
  borderSubtle: 'var(--color-border-subtle)',
  subtle: 'var(--color-subtle)',
};

const MODEL_BAR_COLORS = [
  'var(--color-chart-1)',
  'var(--color-chart-2)',
  'var(--color-chart-3)',
  'var(--color-chart-4)',
  'var(--color-chart-5)',
  'var(--color-chart-6)',
  'var(--color-chart-7)',
  'var(--color-chart-8)',
];

// ── Helpers ───────────────────────────────────────────────────────────────────

// ── Component ─────────────────────────────────────────────────────────────────

export function CostByModelChart() {
  // One bar per chart token; models beyond the top bars aggregate into "Other"
  // so no spend is hidden and series colours are never cycled.
  const sorted: Array<{ model: string; cost: number; isOther?: boolean }> = [...MODEL_COSTS].sort(
    (a, b) => b.cost - a.cost,
  );
  const data =
    sorted.length <= MODEL_BAR_COLORS.length
      ? sorted
      : [
          ...sorted.slice(0, MODEL_BAR_COLORS.length - 1),
          {
            model: 'Other',
            cost: sorted.slice(MODEL_BAR_COLORS.length - 1).reduce((s, row) => s + row.cost, 0),
            isOther: true,
          },
        ];

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
                  fill={entry.isOther ? COLORS.subtle : MODEL_BAR_COLORS[index]}
                />
              ))}
            </Bar>
          </BarChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  );
}
