'use client';

/** @tier 3 · agents/[slug]-scoped. Run counts by duration, capped at p99 with an overflow bar, the median bar brighter and p95 marked when the range is capped. */

import { Bar, BarChart, Cell, ReferenceLine, XAxis, YAxis } from 'recharts';
import { ChartContainer, Stack, Text, donutSegmentColor } from '@orbit/terra';
import { formatDuration } from '../../_types';
import type { RunDurations } from '../../_lib/overview';

/** Sized so the tile, with its single axis line, is as tall as the tiles beside it. */
const CHART_HEIGHT = 62;

const CHART_CONFIG = { runs: { label: 'Runs', color: 'var(--color-accent)' } };

/** Axis labels drop empty trailing units: 29m, not 29m 0s. */
const compact = (ms: number) => formatDuration(ms).replace(/ 0[ms]$/, '');

/** The cap label is approximate, so it rounds to whole minutes and stays short. */
const capLabel = (ms: number) => (ms >= 60_000 ? `${Math.round(ms / 60_000)}m` : compact(ms));

export function DurationHistogram({ durations }: { durations: RunDurations }) {
  const { finished, medianMs, p95Ms, p99Ms, longestMs, capped, buckets, overflow } = durations;
  // The bars span 0 to p99, or to the longest run when there are too few runs for p99 to mean anything.
  const range = capped ? p99Ms : longestMs;
  const bucketOf = (ms: number) => (range > 0 ? Math.min(9, Math.floor((ms / range) * 10)) : -1);
  const medianBucket = bucketOf(medianMs);
  const p95Bucket = bucketOf(p95Ms);
  const data = [...buckets, ...(overflow > 0 ? [overflow] : [])].map((count, i) => ({
    bucket: String(i),
    count,
  }));
  // The p95 label sits left of its line in the right half, and needs room from the median caption.
  const showMedian = finished > 0 && (!capped || p95Bucket - medianBucket >= 5);

  return (
    <Stack
      gap="tight"
      role="img"
      aria-label={
        finished > 0
          ? `Run durations, ${finished} finished runs. Median ${formatDuration(medianMs)}${capped ? `, p95 ${formatDuration(p95Ms)}` : ''}, longest ${formatDuration(longestMs)}.`
          : 'Run durations, no finished runs.'
      }
    >
      <ChartContainer config={CHART_CONFIG} height={CHART_HEIGHT}>
        <BarChart data={data} margin={{ top: 0, right: 0, bottom: 0, left: 0 }} barCategoryGap={2}>
          <XAxis dataKey="bucket" hide />
          {/* Headroom above the tallest bar keeps the marker labels off the bars. */}
          <YAxis hide domain={[0, (max: number) => Math.max(1, max * 1.5)]} />
          <ReferenceLine y={0} stroke="var(--color-border)" />
          <Bar
            dataKey="count"
            radius={[2, 2, 0, 0]}
            isAnimationActive={false}
            minPointSize={(value: number | null | undefined) => (value && value > 0 ? 2 : 0)}
          >
            {data.map((d, i) => (
              <Cell
                key={d.bucket}
                fill={
                  i === 10 ? 'var(--color-subtle)' : donutSegmentColor(i === medianBucket ? 0 : 1)
                }
              />
            ))}
          </Bar>
          {showMedian && (
            <ReferenceLine
              x={String(medianBucket)}
              stroke="transparent"
              label={{
                value: 'median',
                position: medianBucket >= 8 ? 'insideTopRight' : 'insideTop',
                fill: 'var(--color-foreground)',
                fontSize: 10,
              }}
            />
          )}
          {capped && (
            <ReferenceLine
              x={String(p95Bucket)}
              stroke="var(--color-muted)"
              strokeDasharray="3 2"
              label={{
                value: `p95 ${formatDuration(p95Ms)}`,
                position: p95Bucket >= 6 ? 'insideTopRight' : 'insideTopLeft',
                fill: 'var(--color-muted)',
                fontSize: 10,
              }}
            />
          )}
        </BarChart>
      </ChartContainer>
      <Stack direction="horizontal" align="center" justify="between">
        <Text size="caption" color="subtle">
          0s
        </Text>
        <Stack direction="horizontal" align="center" gap="tight">
          {overflow > 0 && (
            <Text size="caption" color="subtle">
              {`>${capLabel(p99Ms)}`}
            </Text>
          )}
          <Text size="caption" color="muted">
            Longest
          </Text>
          <Text mono tabular size="caption">
            {finished > 0 ? compact(longestMs).replace(' ', '\u00a0') : '–'}
          </Text>
        </Stack>
      </Stack>
    </Stack>
  );
}
