/** @tier 3 — triggers-scoped. 24h delivery-outcome sparkline for a trigger row. */

'use client';

import { Bar, BarChart, XAxis, YAxis } from 'recharts';
import { ChartContainer, Stack, Text, Tooltip, TooltipContent, TooltipTrigger } from '@orbit/terra';
import type { ChartConfig } from '@orbit/terra';
import type { SparkBucket } from '../_types/fleet';

interface TriggerSparklineProps {
  buckets: SparkBucket[];
  /** Accessible summary, e.g. the trigger's name. */
  label: string;
}

/**
 * Two series, stacked. Failures sit on top of successes in the same column
 * rather than beside them, so a bar that is mostly red reads as "this mostly
 * failed" without needing a legend at this size.
 *
 * Outcomes that are neither (filtered, superseded, paused) are deliberately not
 * plotted: they are the configuration working, and drawing them would make a
 * correctly-filtered trigger look busy with problems.
 */
const CHART_CONFIG = {
  started: { label: 'Started', color: 'var(--color-status-healthy)' },
  failed: { label: 'Failed', color: 'var(--color-status-error)' },
} satisfies ChartConfig;

/** Row height. Small: this lives inside a table cell. */
const SPARK_HEIGHT = 28;

/**
 * TriggerSparkline shows a trigger's last 24 hours at a glance.
 *
 * Every bucket is plotted, including empty ones. A quiet trigger should read as
 * quiet rather than produce a narrower chart than a busy neighbour, which would
 * make the column impossible to compare down the table.
 */
export function TriggerSparkline({ buckets, label }: TriggerSparklineProps) {
  const totals = buckets.reduce(
    (acc, b) => ({
      started: acc.started + b.started,
      failed: acc.failed + b.failed,
      other: acc.other + b.other,
    }),
    { started: 0, failed: 0, other: 0 },
  );

  if (totals.started + totals.failed + totals.other === 0) {
    return (
      <Text size="caption" color="muted">
        No activity
      </Text>
    );
  }

  const data = buckets.map((b, i) => ({ bucket: i, started: b.started, failed: b.failed }));

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Stack
          aria-label={`${label}: ${totals.started} started, ${totals.failed} failed in the last 24 hours`}
        >
          <ChartContainer config={CHART_CONFIG} height={SPARK_HEIGHT}>
            <BarChart
              data={data}
              margin={{ top: 2, right: 0, bottom: 0, left: 0 }}
              barCategoryGap={1}
            >
              {/* Hidden axes: recharts needs them to establish the scales the
                  bars are measured against, but a sparkline shows no gridlines
                  or labels — the shape is the whole message. */}
              <XAxis dataKey="bucket" hide />
              <YAxis hide />
              <Bar
                dataKey="started"
                stackId="a"
                fill="var(--color-started)"
                isAnimationActive={false}
              />
              <Bar
                dataKey="failed"
                stackId="a"
                fill="var(--color-failed)"
                isAnimationActive={false}
              />
            </BarChart>
          </ChartContainer>
        </Stack>
      </TooltipTrigger>
      <TooltipContent>
        <Text size="supporting">
          Last 24h: {totals.started} started
          {totals.failed > 0 ? `, ${totals.failed} failed` : ''}
          {totals.other > 0 ? `, ${totals.other} not run` : ''}
        </Text>
      </TooltipContent>
    </Tooltip>
  );
}
