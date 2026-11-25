'use client';

/**
 * @tier 3 — agents-scoped. One run as a list row: state icon, title over a meta line, source
 * chips, who started it and when. Used by the agent detail page's Runs tab and Recent Runs, and
 * the Agents page's Running now card.
 */

import { Fragment } from 'react';
import { Stack, Text } from '@runkora/hades';
import { RunSourceChips, RunStateIcon, StarterMark } from '@runkora/components';
import { fmtUsd } from '@runkora/utils';
import {
  formatDuration,
  formatRelativeTime,
  runTitle,
  shortElapsed,
  shortRelativeTime,
} from '../_types';
import type { RunRow } from '../_types';

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
  const title = runTitle(row);
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
