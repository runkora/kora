'use client';

/** @tier 3 · agents/[slug]-scoped. Spend and budget for one agent. */

import { useState } from 'react';
import {
  Alert,
  Card,
  CardContent,
  CardHeader,
  SegmentedControl,
  Separator,
  Stack,
  Text,
} from '@orbit/hades';
import { fmtUsd } from '@orbit/utils';
import { AgentBudgets, Breakdown, TrendChart } from '../../_components/agents-spend-card';
import { SPEND_ALERT_PCT, changeText, usedShare } from '../../_lib/spend';
import type { BudgetRow, SpendRow } from '../../_lib/spend';

/** A model id without its vendor prefix, which would otherwise truncate in the narrow rail. */
export const shortModel = (id: string) => id.replace(/^claude-/, '');

export interface AgentSpendCardProps {
  models: SpendRow[];
  trend: { labels: string[]; values: number[] };
  total: number;
  previousTotal: number;
  /** Change on the previous window as a percentage; null when that window had no spend. */
  change: number | null;
  /** The window the card covers, for example "last 7 days". */
  periodText: string;
  /** The window before it, for example "previous 7 days". */
  previousText: string;
  /** The agent's own budget; null hides the Budget view. */
  budget: BudgetRow | null;
}

export function AgentSpendCard({
  models,
  trend,
  total,
  previousTotal,
  change,
  periodText,
  previousText,
  budget,
}: AgentSpendCardProps) {
  const windows = budget?.windows ?? [];
  const perRunLimit = budget?.perRunLimit ?? 0;
  const hasBudget = windows.length > 0 || perRunLimit > 0;
  const views = [
    { value: 'model', label: 'Model' },
    { value: 'trend', label: 'Trend' },
    ...(hasBudget ? [{ value: 'budget', label: 'Budget' }] : []),
  ];
  const [chosen, setChosen] = useState('model');
  const view = views.some((v) => v.value === chosen) ? chosen : 'model';

  const reached = windows.some((w) => usedShare(w.spent, w.limit) >= 1);
  // One row per window so each bar is labelled by its cadence, not by the agent.
  const budgetRows: BudgetRow[] = windows.map((w) => ({
    name: `${w.cadence === 'weekly' ? 'Weekly' : 'Monthly'} budget`,
    windows: [w],
  }));

  return (
    <Card column>
      <CardHeader
        compact
        title="Spend"
        description={
          view === 'budget'
            ? 'Limits'
            : `${periodText.charAt(0).toUpperCase()}${periodText.slice(1)}`
        }
        divider
        action={
          <SegmentedControl
            aria-label="Break spend down by"
            value={view}
            onChange={setChosen}
            options={views}
          />
        }
      />
      <CardContent padded grow>
        <Stack gap="comfortable" align="stretch">
          {view === 'budget' ? (
            <>
              {reached && (
                <Alert variant="error">
                  Budget ceiling reached. This agent&apos;s inference requests are blocked until the
                  period resets or its limit is raised.
                </Alert>
              )}
              <AgentBudgets rows={budgetRows} />
              {perRunLimit > 0 && (
                <Stack direction="horizontal" align="baseline" justify="between">
                  <Text size="caption" color="muted">
                    Per-run limit
                  </Text>
                  <Text mono size="caption">
                    {fmtUsd(perRunLimit)} a run
                  </Text>
                </Stack>
              )}
            </>
          ) : total === 0 ? (
            <Text size="supporting" color="muted">
              No spend in this window yet.
            </Text>
          ) : (
            <>
              {view === 'trend' ? (
                <TrendChart labels={trend.labels} values={trend.values} />
              ) : (
                <Breakdown
                  rows={models.map((m) => ({ ...m, name: shortModel(m.name) }))}
                  total={total}
                />
              )}
              {change !== null && (
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
                      {changeText(change)} · {fmtUsd(previousTotal)}
                    </Text>
                  </Stack>
                </>
              )}
            </>
          )}
        </Stack>
      </CardContent>
    </Card>
  );
}
