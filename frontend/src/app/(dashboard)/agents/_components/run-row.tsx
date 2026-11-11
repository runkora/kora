'use client';

/**
 * @tier 3 — agents-scoped. One run as a list row: state icon, title over a meta line, source
 * chips, who started it and when. Used by the agent detail page's Runs tab and Recent Runs, and
 * the Agents page's Running now card.
 */

import { Fragment } from 'react';
import {
  Avatar,
  Icon,
  Stack,
  Text,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
  type IconColor,
  type IconName,
} from '@runkora/hades';
import { RUN_STATE_DISPLAY, RunSourceChips, type RunStateDisplay } from '@runkora/components';
import { fmtUsd } from '@runkora/utils';
import { formatDuration, formatRelativeTime } from '../_types';
import type { RunRow } from '../_types';
import type { TriggerKind } from '@runkora/shared/api';

const STATE_ICON: Partial<Record<RunStateDisplay['status'], { name: IconName; color: IconColor }>> =
  {
    healthy: { name: 'loader', color: 'status-healthy' },
    pending: { name: 'clock', color: 'status-pending' },
    degraded: { name: 'pause', color: 'status-degraded' },
    error: { name: 'circle-x', color: 'status-error' },
    idle: { name: 'circle-check', color: 'muted' },
  };

/** The run's state as an icon, named for screen readers with the state's label. */
function RunStateIcon({ state }: { state: string }) {
  const display = RUN_STATE_DISPLAY[state] ?? { status: 'idle', label: state };
  const icon = STATE_ICON[display.status] ?? { name: 'circle-check', color: 'muted' };
  return (
    <Icon
      name={icon.name}
      size="sm"
      color={icon.color}
      label={display.label}
      shrink
      spin={icon.name === 'loader'}
    />
  );
}

const TRIGGER_KIND_ICON: Record<TriggerKind, IconName> = {
  webhook: 'zap',
  scheduled: 'clock',
  manual: 'play',
  event: 'activity',
};

/** The icon for a trigger of the given kind; `zap` for a run recorded without one. */
export function triggerKindIcon(kind: TriggerKind | null): IconName {
  return (kind && TRIGGER_KIND_ICON[kind]) ?? 'zap';
}

/** Who started the run as one round mark: a person's initials or the trigger kind's icon. */
export function StarterMark({ row }: { row: RunRow }) {
  const trigger = row.startedByTrigger;
  const name = trigger
    ? (trigger.triggerName ?? trigger.triggerId)
    : (row.createdByName ?? row.createdBy);
  const tooltip = `Started by ${name}`;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Text as="span" aria-label={tooltip} style={{ display: 'inline-flex', flexShrink: 0 }}>
          {trigger ? (
            <Icon
              name={triggerKindIcon(trigger.triggerKind)}
              size="2xs"
              container
              circle
              containerVariant="accent"
            />
          ) : (
            <Avatar name={name} size="2xs" />
          )}
        </Text>
      </TooltipTrigger>
      <TooltipContent>{tooltip}</TooltipContent>
    </Tooltip>
  );
}

/** "4m", "3h", "2d" for a span of seconds, or `under` below a minute. */
function shortSpan(seconds: number, under: string): string {
  if (seconds < 60) return under;
  const m = Math.floor(seconds / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h`;
  return `${Math.floor(h / 24)}d`;
}

/** "now", "4m", "3h", "2d": how long ago, short enough for a row's right edge. */
export function shortRelativeTime(iso: string, now = Date.now()): string {
  return shortSpan(Math.max(0, Math.floor((now - new Date(iso).getTime()) / 1000)), 'now');
}

/** "45s", "4m", "3h": how long a run has taken, as short as shortRelativeTime. */
export function shortElapsed(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  return shortSpan(s, `${s}s`);
}

/** The row's title: the task summary, or what is known before the agent writes one. */
function titleOf(row: RunRow): { text: string; known: boolean } {
  if (row.summary) return { text: row.summary, known: true };
  if (row.state === 'pending' || row.state === 'starting')
    return { text: 'Waiting to start', known: false };
  return { text: 'No task summary', known: false };
}

/** The right-edge time, short, with the full value for its tooltip; a dash before the run starts. */
function rowTime(row: RunRow, time: 'started' | 'elapsed'): { text: string; title?: string } {
  if (time === 'elapsed') {
    if (row.durationMs === null) return { text: '—' };
    return { text: shortElapsed(row.durationMs), title: formatDuration(row.durationMs) };
  }
  if (!row.startedAt) return { text: '—' };
  return { text: shortRelativeTime(row.startedAt), title: formatRelativeTime(row.startedAt) };
}

export interface RunListRowProps {
  row: RunRow;
  /** How many delivery chips show before the rest fold into "+N". */
  maxChips?: number;
  /** Lead the meta line with the agent's name, for a list that spans agents. */
  showAgent?: boolean;
  /** The right-edge time: when the run started, or how long it has run. */
  time?: 'started' | 'elapsed';
  /** A trailing control, such as Delete. */
  action?: React.ReactNode;
}

export function RunListRow({
  row,
  maxChips,
  showAgent = false,
  time = 'started',
  action,
}: RunListRowProps) {
  const title = titleOf(row);
  const metaParts = [
    ...(showAgent ? [row.agentTypeName] : []),
    row.id.slice(0, 8),
    formatDuration(row.durationMs),
    ...(row.costUsd != null ? [fmtUsd(row.costUsd)] : []),
  ];
  const when = rowTime(row, time);
  return (
    // The end padding keeps a trailing control's focus ring inside the cell, which clips overflow.
    <Stack
      direction="horizontal"
      align="center"
      gap="cozy"
      grow
      minWidth="none"
      style={{ paddingInlineEnd: 4 }}
    >
      <RunStateIcon state={row.state} />
      <Stack gap="tight" grow minWidth="form-field-sm">
        {title.known ? (
          <Text size="body" weight="emphasis" truncate title={title.text}>
            {title.text}
          </Text>
        ) : (
          <Text size="body" color="subtle" truncate>
            {title.text}
          </Text>
        )}
        {/* One text run, so a narrow row ends it with an ellipsis instead of clipping a glyph. */}
        <Text as="span" mono size="caption" color="muted" truncate title={metaParts.join(' · ')}>
          {metaParts.map((part, i) => (
            <Fragment key={i}>
              {/* The dots are in the body face, which keeps them narrower than mono. */}
              {i > 0 && (
                <Text as="span" size="caption" color="subtle">
                  {' · '}
                </Text>
              )}
              {/* An agent name is prose, so it keeps the body face. */}
              {showAgent && i === 0 ? (
                <Text as="span" size="caption" color="muted">
                  {part}
                </Text>
              ) : (
                part
              )}
            </Fragment>
          ))}
        </Text>
      </Stack>
      <Stack direction="horizontal" align="center" gap="cozy" minWidth="none">
        <Stack direction="horizontal" justify="end" minWidth="none" maxWidth="form-field">
          <RunSourceChips sources={row.sources} max={maxChips} />
        </Stack>
        <Stack direction="horizontal" align="center" gap="tight" shrink={false}>
          <StarterMark row={row} />
          {/* Fixed to the widest short time, so the marks line up down the list. */}
          <Text
            size="caption"
            color="muted"
            align="end"
            tabular
            title={when.title}
            style={{ width: '1.75rem', flexShrink: 0 }}
          >
            {when.text}
          </Text>
        </Stack>
      </Stack>
      {/* Outside the right-hand group, which clips to its own height, so the focus ring shows whole. */}
      {action}
    </Stack>
  );
}
