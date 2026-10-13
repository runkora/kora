'use client';

/** @tier 3 — agents-scoped. The four headline figures above the agent list. */

import {
  Card,
  CardContent,
  CardFooter,
  Eyebrow,
  Grid,
  Heading,
  Icon,
  Stack,
  Text,
  donutSegmentColor,
} from '@runkora/hades';
import type { IconName } from '@runkora/hades';
import { fmtTokens } from '@orbit/utils';
import { formatDuration } from '../_types';

interface TileSegment {
  label: string;
  value: number;
  /** What the legend shows instead of the raw value, for example a formatted token count. */
  text?: string;
}

/** A proportional bar for the parts of one figure, in the same hue ramp as the spend ring. */
function SegmentBar({ segments }: { segments: TileSegment[] }) {
  return (
    <Stack direction="horizontal" gap="tight" style={{ height: '0.375rem' }}>
      {segments.map((seg, i) =>
        seg.value > 0 ? (
          <Stack
            key={seg.label}
            style={{
              flex: `${seg.value} 1 0`,
              borderRadius: '9999px',
              background: donutSegmentColor(i),
            }}
          />
        ) : null,
      )}
    </Stack>
  );
}

function SegmentLegend({ segments }: { segments: TileSegment[] }) {
  return (
    <Stack direction="horizontal" gap="snug" wrap>
      {segments.map((seg, i) => (
        <Stack key={seg.label} direction="horizontal" align="center" gap="snug">
          <Stack
            shrink
            style={{
              width: '0.5rem',
              height: '0.5rem',
              borderRadius: '9999px',
              background: donutSegmentColor(i),
            }}
          />
          <Text size="caption" color="muted">
            {seg.text ?? seg.value} {seg.label}
          </Text>
        </Stack>
      ))}
    </Stack>
  );
}

interface StatTileProps {
  label: string;
  icon: IconName;
  value: string;
  segments?: TileSegment[];
  /** A custom body shown instead of the bar and legend, for a figure that is not a composition. */
  children?: React.ReactNode;
  /** The period or scope the figure covers, bottom left. */
  period: string;
  /** One extra fact, bottom right. */
  trailing: string;
}

/** Every tile has the same four parts in the same places: figure, bar, legend, footer. */
export function StatTile({
  label,
  icon,
  value,
  segments = [],
  children,
  period,
  trailing,
}: StatTileProps) {
  return (
    <Card column>
      <CardContent padded grow>
        <Stack gap="cozy" align="stretch">
          <Stack direction="horizontal" align="center" justify="between">
            <Eyebrow>{label}</Eyebrow>
            <Icon name={icon} size="xs" />
          </Stack>
          <Heading variant="metric">{value}</Heading>
          {children ?? (
            <>
              <Stack justify="center" style={{ height: '1rem' }}>
                <SegmentBar segments={segments} />
              </Stack>
              <SegmentLegend segments={segments} />
            </>
          )}
        </Stack>
      </CardContent>
      <CardFooter compact justify="between">
        <Text size="caption" color="subtle">
          {period}
        </Text>
        <Text size="caption" color="muted">
          {trailing}
        </Text>
      </CardFooter>
    </Card>
  );
}

export interface AgentStatTilesProps {
  agents: { total: number; healthy: number; provisioning: number; delegated: number };
  /** Null when the run counts could not be read, which the tile shows as a dash. */
  runs: { running: number; starting: number; queued: number; longestMs: number } | null;
  outcomes: { completed: number; failed: number };
  /** Null when the window's usage is not loaded, which the tile shows as a dash. */
  tokens: { cached: number; input: number; output: number } | null;
  /** What the tokens tile says in place of its figures while `tokens` is null. */
  tokensNote: string;
  /** The window the token figures cover, for example "last 7 days". */
  periodText: string;
  /** The window the run outcomes cover. Differs from `periodText` when only recent runs are known. */
  outcomesText: string;
}

export function AgentStatTiles({
  agents,
  runs,
  outcomes,
  tokens: tokenFigures,
  tokensNote,
  periodText,
  outcomesText,
}: AgentStatTilesProps) {
  const tokens = tokenFigures ?? { cached: 0, input: 0, output: 0 };
  const counts = runs ?? { running: 0, starting: 0, queued: 0, longestMs: 0 };
  const live = counts.running + counts.starting + counts.queued;
  const finished = outcomes.completed + outcomes.failed;
  const successRate = finished > 0 ? Math.round((outcomes.completed / finished) * 100) : 0;
  const totalTokens = tokens.cached + tokens.input + tokens.output;
  const promptSide = tokens.cached + tokens.input;
  const cachedShare = promptSide > 0 ? Math.round((tokens.cached / promptSide) * 100) : 0;

  return (
    <Grid cols={{ base: 1, sm: 2 }} gap="comfortable">
      <StatTile
        label="Agents"
        icon="bot"
        value={String(agents.total)}
        segments={[
          { label: 'healthy', value: agents.healthy },
          { label: 'provisioning', value: agents.provisioning },
        ]}
        period="in this workspace"
        trailing={`${agents.delegated} delegated`}
      />
      <StatTile
        label="Active runs"
        icon="activity"
        value={runs ? String(live) : '–'}
        segments={[
          { label: 'running', value: counts.running },
          { label: 'starting', value: counts.starting },
          { label: 'queued', value: counts.queued },
        ]}
        period="right now"
        trailing={
          !runs
            ? 'Unavailable'
            : live > 0
              ? `Longest ${formatDuration(counts.longestMs)}`
              : 'Nothing running'
        }
      />
      <StatTile
        label="Completed runs"
        icon="circle-check"
        value={String(outcomes.completed)}
        segments={[
          { label: 'completed', value: outcomes.completed },
          { label: 'failed', value: outcomes.failed },
        ]}
        period={outcomesText}
        trailing={finished > 0 ? `${successRate}% success` : 'No runs yet'}
      />
      <StatTile
        label="Tokens"
        icon="zap"
        value={tokenFigures === null ? '–' : totalTokens > 0 ? fmtTokens(totalTokens) : '0'}
        segments={[
          { label: 'cached', value: tokens.cached, text: fmtTokens(tokens.cached) },
          { label: 'input', value: tokens.input, text: fmtTokens(tokens.input) },
          { label: 'output', value: tokens.output, text: fmtTokens(tokens.output) },
        ]}
        period={periodText}
        trailing={
          tokenFigures === null
            ? tokensNote
            : promptSide > 0
              ? `${cachedShare}% cached`
              : 'No usage yet'
        }
      />
    </Grid>
  );
}
