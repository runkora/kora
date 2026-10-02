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
} from '@orbit/terra';
import { AppLink } from '@orbit/components/links';
import { fmtUsd } from '@orbit/utils';
import { budgetTone } from '../_lib/spend';
import type { BudgetRow, SpendRow, TenantBudget } from '../_lib/spend';

const CHART_CONFIG = { cost: { label: 'Spend', color: 'var(--color-accent)' } };

/** Spend over time as a scaled area chart with a dollar axis, an average line and the peak marked. */
function TrendChart({ labels, values }: { labels: string[]; values: number[] }) {
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
function Breakdown({ rows, total }: { rows: SpendRow[]; total: number }) {
  return (
    <Stack direction="horizontal" gap="relaxed" align="center">
      <Donut
        segments={rows.map((r) => ({ label: r.name, value: r.cost }))}
        size={104}
        thickness={10}
      >
        <Text mono size="supporting" weight="emphasis">
          {fmtUsd(total)}
        </Text>
      </Donut>
      <Stack grow gap="snug" align="stretch">
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
            <Stack grow>
              <Text size="caption" color="muted" truncate>
                {row.name}
              </Text>
            </Stack>
            <Text mono size="caption" color="subtle">
              {total > 0 ? Math.round((row.cost / total) * 100) : 0}%
            </Text>
            <Text mono size="caption">
              {fmtUsd(row.cost)}
            </Text>
          </Stack>
        ))}
      </Stack>
    </Stack>
  );
}

/** Each agent's month-to-date spend against its own limit. Agents without a budget show spend only. */
function AgentBudgets({ rows }: { rows: BudgetRow[] }) {
  return (
    <Stack gap="cozy" align="stretch">
      {rows.map((row) => {
        const used = row.limit ? Math.round((row.monthToDate / row.limit) * 100) : null;
        return (
          <Stack key={row.name} gap="tight" align="stretch">
            <Stack direction="horizontal" align="baseline" gap="snug">
              <Stack grow>
                <Text size="caption" color="muted" truncate>
                  {row.name}
                </Text>
              </Stack>
              <Stack align="end" style={{ minWidth: '4.5rem' }}>
                <Text
                  mono
                  size="caption"
                  color={used !== null && used >= 80 ? 'status-degraded' : 'foreground'}
                >
                  {fmtUsd(row.monthToDate)}
                </Text>
              </Stack>
              <Stack align="end" style={{ minWidth: '5.5rem' }}>
                <Text mono size="caption" color="subtle">
                  {row.limit ? `of ${fmtUsd(row.limit)}` : 'no budget'}
                </Text>
              </Stack>
            </Stack>
            <Progress
              value={Math.min(used ?? 0, 100)}
              label={`${row.name} budget used`}
              variant={used === null ? 'default' : budgetTone(used)}
              size="hairline"
            />
          </Stack>
        );
      })}
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
  budgets?: BudgetRow[] | undefined;
  tenantBudget?: TenantBudget | undefined;
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
  tenantBudget,
}: AgentsSpendCardProps) {
  const views = [
    { value: 'agent', label: 'Agent' },
    ...(models && models.length > 0 ? [{ value: 'model', label: 'Model' }] : []),
    ...(trend ? [{ value: 'trend', label: 'Trend' }] : []),
    ...(budgets && budgets.length > 0 ? [{ value: 'budgets', label: 'Budgets' }] : []),
  ];
  const [chosen, setChosen] = useState('agent');
  const view = views.some((v) => v.value === chosen) ? chosen : 'agent';

  const used = tenantBudget ? Math.round((tenantBudget.monthToDate / tenantBudget.limit) * 100) : 0;
  const change =
    previousTotal !== undefined && previousTotal > 0
      ? Math.round(((total - previousTotal) / previousTotal) * 100)
      : null;

  return (
    <Card column>
      <CardHeader
        title="Spend"
        description={
          view === 'budgets'
            ? 'This month'
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
          {agents.length === 0 ? (
            <Text size="supporting" color="muted">
              No spend in this window yet.
            </Text>
          ) : view === 'budgets' && budgets ? (
            <AgentBudgets rows={budgets} />
          ) : view === 'trend' && trend ? (
            <TrendChart labels={trend.labels} values={trend.values} />
          ) : view === 'model' && models ? (
            <Breakdown rows={models} total={total} />
          ) : (
            <Breakdown rows={agents} total={total} />
          )}
          {tenantBudget && (
            <>
              <Separator />
              <Stack gap="snug" align="stretch">
                <Stack direction="horizontal" justify="between" align="baseline">
                  <Text size="caption" color="muted">
                    Monthly budget
                  </Text>
                  <Text size="caption" color="subtle">
                    {fmtUsd(tenantBudget.monthToDate)} of {fmtUsd(tenantBudget.limit)}
                  </Text>
                </Stack>
                <Progress
                  value={Math.min(used, 100)}
                  label="Monthly budget used"
                  variant={budgetTone(used)}
                  size="sm"
                />
                <Text size="caption" color="muted">
                  {fmtUsd(tenantBudget.limit - tenantBudget.monthToDate)} left ·{' '}
                  {tenantBudget.daysLeft} days to go
                </Text>
              </Stack>
            </>
          )}
          {change !== null && view !== 'budgets' && (
            <>
              <Separator />
              <Stack direction="horizontal" align="baseline" justify="between">
                <Text size="caption" color="muted">
                  Versus {previousText}
                </Text>
                <Text mono size="caption" color={change > 25 ? 'status-degraded' : 'foreground'}>
                  {change >= 0 ? '▲' : '▼'} {Math.abs(change)}% · {fmtUsd(previousTotal ?? 0)}{' '}
                  before
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
