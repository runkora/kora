'use client';

/** @tier 3 — agents-scoped. Spend for the window, split by agent and, when the data exists, by model, over time and against budgets. */

import { useState } from 'react';
import {
  Area,
  AreaChart,
  CartesianGrid,
  ReferenceDot,
  ReferenceLine,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  ChartContainer,
  ChartTooltipContent,
  Donut,
  Progress,
  SegmentedControl,
  Separator,
  Stack,
  Text,
  donutSegmentColor,
} from '@runkora/hades';
import { AppLink } from '@orbit/components/links';
import { fmtDate, fmtUsd } from '@orbit/utils';
import {
  BUDGET_ROWS,
  SPEND_ALERT_PCT,
  budgetTone,
  changeText,
  groupTail,
  spendChange,
  usedShare,
} from '../_lib/spend';
import type { BudgetRow, SpendRow } from '../_lib/spend';

const CHART_CONFIG = { cost: { label: 'Spend', color: 'var(--color-accent)' } };

/** Spend over time as a scaled area chart with a dollar axis, an average line and the peak marked. */
export function TrendChart({ labels, values }: { labels: string[]; values: number[] }) {
  const data = values.map((cost, i) => ({ label: labels[i] ?? String(i + 1), cost }));
  const average = values.reduce((n, v) => n + v, 0) / Math.max(values.length, 1);
  const peak = data.reduce(
    (top, d) => (d.cost > top.cost ? d : top),
    data[0] ?? { label: '', cost: 0 },
  );
  const top = Math.max(10, Math.ceil(peak.cost / 10) * 10);

  return (
    <ChartContainer config={CHART_CONFIG} height={170}>
      <AreaChart data={data} margin={{ top: 8, right: 12, bottom: 0, left: 0 }}>
        <defs>
          <linearGradient id="agents-spend-fill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="var(--color-accent)" stopOpacity={0.35} />
            <stop offset="100%" stopColor="var(--color-accent)" stopOpacity={0} />
          </linearGradient>
        </defs>
        <CartesianGrid strokeDasharray="3 3" stroke="var(--color-border-subtle)" vertical={false} />
        <XAxis
          dataKey="label"
          tick={{ fill: 'var(--color-muted)', fontSize: 11 }}
          tickLine={false}
          axisLine={false}
          padding={{ left: 8, right: 8 }}
        />
        <YAxis
          tick={{ fill: 'var(--color-muted)', fontSize: 11 }}
          tickLine={false}
          axisLine={false}
          tickFormatter={(v: number) => `$${v}`}
          width={36}
          domain={[0, top]}
          ticks={[0, top / 2, top]}
        />
        <Tooltip
          cursor={{ stroke: 'var(--color-border)', strokeWidth: 1 }}
          content={<ChartTooltipContent formatter={(v) => fmtUsd(Number(v))} />}
        />
        <ReferenceLine
          y={average}
          stroke="var(--color-subtle)"
          strokeDasharray="4 3"
          label={{
            value: `avg ${fmtUsd(average)}`,
            position: 'insideBottomRight',
            fill: 'var(--color-subtle)',
            fontSize: 10,
          }}
        />
        <Area
          type="monotone"
          dataKey="cost"
          stroke="var(--color-accent)"
          strokeWidth={2}
          fill="url(#agents-spend-fill)"
          dot={{ r: 3, fill: 'var(--color-accent)', strokeWidth: 0 }}
          activeDot={{ r: 5 }}
        />
        <ReferenceDot
          x={peak.label}
          y={peak.cost}
          r={5}
          fill="var(--color-accent)"
          stroke="var(--color-surface)"
          strokeWidth={2}
        />
      </AreaChart>
    </ChartContainer>
  );
}

/** A ring and a legend for one breakdown of the window's spend. */
export function Breakdown({
  rows: allRows,
  total,
  deleted = 0,
  format = fmtUsd,
}: {
  rows: SpendRow[];
  total: number;
  /** How amounts read; dollars unless the rows are another unit, such as tokens. */
  format?: (value: number) => string;
  /** Spend of agents that no longer exist: it is in the total, so it gets a row. */
  deleted?: number;
}) {
  const rows = [
    ...groupTail(allRows),
    ...(deleted > 0 ? [{ name: 'Deleted agents', cost: deleted }] : []),
  ];
  return (
    <Stack direction="horizontal" gap="relaxed" align="center">
      <Donut
        segments={rows.map((r) => ({ label: r.name, value: r.cost }))}
        size={104}
        thickness={10}
      >
        <Text mono size="supporting" weight="emphasis">
          {format(total)}
        </Text>
      </Donut>
      <Stack grow gap="snug" align="stretch" minWidth="none">
        {rows.map((row, i) => (
          <Stack key={row.name} direction="horizontal" align="center" gap="snug">
            <Stack
              shrink
              style={{
                width: '0.5rem',
                height: '0.5rem',
                borderRadius: '9999px',
                background: donutSegmentColor(i),
              }}
            />
            <Stack grow minWidth="none">
              <Text size="caption" color="muted" truncate>
                {row.name}
              </Text>
            </Stack>
            <Text mono size="caption" color="subtle">
              {total > 0 ? Math.round((row.cost / total) * 100) : 0}%
            </Text>
            <Text mono size="caption">
              {format(row.cost)}
            </Text>
          </Stack>
        ))}
      </Stack>
    </Stack>
  );
}

/**
 * A bar per budget window of each agent. Rows arrive closest to a limit first, and
 * those past BUDGET_ROWS are counted.
 */
export function AgentBudgets({ rows: allRows }: { rows: BudgetRow[] }) {
  const rows = allRows.slice(0, BUDGET_ROWS);
  const hidden = allRows.length - rows.length;
  return (
    <Stack gap="cozy" align="stretch">
      {rows.flatMap((row) =>
        row.windows.map((w) => {
          const used = Math.round(usedShare(w.spent, w.limit) * 100);
          const weekly = w.cadence === 'weekly';
          const label = row.windows.length > 1 ? `${row.name} (${w.cadence})` : row.name;
          return (
            <Stack key={`${row.name}-${w.cadence}`} gap="tight" align="stretch">
              <Stack direction="horizontal" align="baseline" gap="snug">
                <Stack grow>
                  <Text size="caption" color="muted" truncate>
                    {label}
                  </Text>
                </Stack>
                <Stack align="end" style={{ minWidth: '4.5rem' }}>
                  <Text
                    mono
                    size="caption"
                    color={budgetTone(used) === 'healthy' ? 'foreground' : 'status-degraded'}
                  >
                    {fmtUsd(w.spent)}
                  </Text>
                </Stack>
                <Stack align="end" style={{ minWidth: '5.5rem' }}>
                  <Text mono size="caption" color="subtle">
                    {`of ${fmtUsd(w.limit)}${weekly ? ' a week' : ''}`}
                  </Text>
                </Stack>
              </Stack>
              <Progress
                value={Math.min(used, 100)}
                label={`${row.name} ${w.cadence} budget used, resets ${fmtDate(w.resetsAt)}`}
                variant={budgetTone(used)}
                size="hairline"
              />
            </Stack>
          );
        }),
      )}
      {hidden > 0 && (
        <Text size="caption" color="subtle">
          +{hidden} more {hidden === 1 ? 'agent' : 'agents'} not shown
        </Text>
      )}
      <Text size="caption" color="subtle">
        Spend is counted by agent, so it can differ slightly from what the limit counts.
      </Text>
    </Stack>
  );
}

export interface AgentsSpendCardProps {
  /** Spend per agent, largest first. */
  agents: SpendRow[];
  total: number;
  /** The window the card covers, for example "last 7 days". */
  periodText: string;
  /** The window before it, for example "previous 7 days". */
  previousText: string;
  models?: SpendRow[] | undefined;
  trend?: { labels: string[]; values: number[] } | undefined;
  previousTotal?: number | undefined;
  /** One row per agent with a limit, the closest to a limit first. */
  budgets?: BudgetRow[] | undefined;
  /** Spend in the window by agents that have since been deleted. It is part of `total`. */
  deletedCost?: number | undefined;
  /** Replaces "No spend in this window yet." when the window's figures are not loaded. */
  emptyText?: string | undefined;
}

/**
 * One card with a view per breakdown the data supports. The agent split is
 * always there; model, trend and budget views appear only when their data is
 * supplied, and the control disappears when only one view is left.
 */
export function AgentsSpendCard({
  agents,
  total,
  periodText,
  previousText,
  models,
  trend,
  previousTotal,
  budgets,
  emptyText,
  deletedCost,
}: AgentsSpendCardProps) {
  const views = [
    { value: 'agent', label: 'Agent' },
    ...(models && models.length > 0 ? [{ value: 'model', label: 'Model' }] : []),
    ...(trend ? [{ value: 'trend', label: 'Trend' }] : []),
    ...(budgets && budgets.length > 0 ? [{ value: 'budgets', label: 'Budgets' }] : []),
  ];
  const [chosen, setChosen] = useState('agent');
  const view = views.some((v) => v.value === chosen) ? chosen : 'agent';

  const change = spendChange(total, previousTotal);

  return (
    <Card column>
      <CardHeader
        title="Spend"
        description={
          view === 'budgets'
            ? 'Against each limit'
            : `${periodText.charAt(0).toUpperCase()}${periodText.slice(1)}`
        }
        divider
        action={
          views.length > 1 ? (
            <SegmentedControl
              aria-label="Break spend down by"
              value={view}
              onChange={setChosen}
              options={views}
            />
          ) : undefined
        }
      />
      <CardContent padded grow>
        <Stack gap="comfortable" align="stretch" justify="center" style={{ minHeight: '100%' }}>
          {view === 'budgets' && budgets ? (
            <AgentBudgets rows={budgets} />
          ) : agents.length === 0 && !deletedCost ? (
            <Text size="supporting" color="muted">
              {emptyText ?? 'No spend in this window yet.'}
            </Text>
          ) : view === 'trend' && trend ? (
            <TrendChart labels={trend.labels} values={trend.values} />
          ) : view === 'model' && models ? (
            <Breakdown rows={models} total={total} />
          ) : (
            <Breakdown rows={agents} total={total} deleted={deletedCost ?? 0} />
          )}
          {change !== null && view !== 'budgets' && (
            <>
              <Separator />
              <Stack direction="horizontal" align="baseline" justify="between">
                <Text size="caption" color="muted">
                  Versus {previousText}
                </Text>
                <Text
                  mono
                  size="caption"
                  color={change > SPEND_ALERT_PCT ? 'status-degraded' : 'foreground'}
                >
                  {changeText(change)} · {fmtUsd(previousTotal ?? 0)} before
                </Text>
              </Stack>
            </>
          )}
        </Stack>
      </CardContent>
      <CardFooter compact justify="end">
        <AppLink to="/model-gateway/my-usage" size="sm">
          View usage
        </AppLink>
      </CardFooter>
    </Card>
  );
}
