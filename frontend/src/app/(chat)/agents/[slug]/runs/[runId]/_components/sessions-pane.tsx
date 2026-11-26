'use client';

/** @tier 3 — run-page-scoped. The run's sessions and the agent's other runs, beside the thread. */

import type { ReactNode } from 'react';
import Link from 'next/link';
import { useSearchParams } from 'next/navigation';
import { Icon, Stack, Text, type IconName } from '@runkora/hades';
import { APP_ROUTES } from '@runkora/lib/routing';
import type { EarlierRun } from '../_lib/to-run-view';
import { commandParts, formatSpan, type Row, type RunView } from '../_lib/run-view';
import { LINE, NOWRAP } from '../_lib/run-styles';

export function SessionRow({
  label,
  meta,
  href,
  active,
  running,
  indent,
  icon,
}: {
  label: string;
  meta: string;
  href?: string;
  active?: boolean;
  running?: boolean;
  indent?: boolean;
  /** Replaces the state mark, for a row that is an action rather than a run. */
  icon?: IconName;
}) {
  const row = (
    <Stack
      direction="horizontal"
      align="center"
      gap="snug"
      style={{
        padding: '7px 10px',
        paddingLeft: indent ? 28 : 10,
        borderRadius: 6,
        background: active ? 'var(--color-surface-elevated)' : 'transparent',
      }}
    >
      <Icon
        name={icon ?? (running ? 'loader' : 'circle-check')}
        spin={running}
        size="2xs"
        color={running ? 'accent' : 'subtle'}
        label={icon ? '' : running ? 'Running' : 'Done'}
      />
      <Text
        size="supporting"
        color={active ? 'foreground' : 'muted'}
        truncate
        style={{ flex: 1, minWidth: 0 }}
      >
        {label}
      </Text>
      <Text size="caption" color="subtle" style={NOWRAP}>
        {meta}
      </Text>
    </Stack>
  );
  return href ? (
    <Stack asChild>
      <Link href={href} style={{ textDecoration: 'none', color: 'inherit' }}>
        {row}
      </Link>
    </Stack>
  ) : (
    row
  );
}

/** The left pane's column: beside the thread, or filling the drawer it opens in. */
export function PaneFrame({ drawer, children }: { drawer?: boolean; children: ReactNode }) {
  return (
    <Stack
      gap="relaxed"
      shrink={!drawer}
      overflowY="auto"
      style={{
        // In a drawer the pane fills the panel's column and scrolls inside it.
        ...(drawer ? { flex: '1 1 0', minHeight: 0 } : {}),
        width: drawer ? '100%' : 264,
        borderRight: drawer ? undefined : LINE,
        padding: '18px 12px',
      }}
    >
      {children}
    </Stack>
  );
}

/** This run's main session and subagents, then the agent's other runs. */
export function SessionsPane({
  run,
  rows,
  earlier,
  earlierError,
  drawer,
}: {
  run: RunView;
  rows: Row[];
  earlier: EarlierRun[];
  earlierError: string | null;
  /** Fills the drawer it opens in instead of standing beside the thread. */
  drawer?: boolean;
}) {
  const subagents = rows.flatMap((r) =>
    r.type === 'step' && r.step.kind === 'subagent' ? [r.step] : [],
  );
  // Only `from=runs` counts, and the back link's path is fixed, so the URL cannot send it
  // elsewhere. `runs` carries the Runs page's filters back.
  const params = useSearchParams();
  const runsQuery =
    params?.get('from') === 'runs'
      ? new URLSearchParams(params.get('runs') ?? '').toString()
      : undefined;
  const back =
    runsQuery === undefined
      ? APP_ROUTES.agents.detail(run.agent.slug)
      : `${APP_ROUTES.runs}${runsQuery ? `?${runsQuery}` : ''}`;
  return (
    <PaneFrame drawer={drawer}>
      <Stack asChild direction="horizontal" align="center" gap="snug">
        <Link href={back} style={{ padding: '0 10px', textDecoration: 'none', color: 'inherit' }}>
          <Icon name="arrow-left" size="2xs" color="subtle" label="" />
          <Text size="supporting" color="muted">
            {runsQuery === undefined ? run.agent.name : 'Runs'}
          </Text>
        </Link>
      </Stack>
      <Stack gap="tight">
        <Text size="caption" color="subtle" style={{ padding: '0 10px 6px' }}>
          This run
        </Text>
        <SessionRow
          label="Main session"
          meta={formatSpan(run.durationMs)}
          active
          running={run.state === 'active'}
        />
        {subagents.map((s) => (
          <SessionRow
            key={s.id}
            label={commandParts(s)[0]}
            meta={s.status === 'running' ? 'now' : formatSpan(s.durationMs ?? 0)}
            running={s.status === 'running'}
            indent
          />
        ))}
      </Stack>
      {(earlier.length > 0 || earlierError) && (
        <Stack gap="tight">
          <Text size="caption" color="subtle" style={{ padding: '0 10px 6px' }}>
            Other runs
          </Text>
          {earlierError && (
            <Text size="caption" color="status-error" style={{ padding: '0 10px' }}>
              {earlierError}
            </Text>
          )}
          {earlier.map((r) => (
            <SessionRow
              key={r.id}
              label={r.label}
              meta={r.meta}
              running={r.running}
              href={APP_ROUTES.agents.run(run.agent.slug, r.id, runsQuery)}
            />
          ))}
        </Stack>
      )}
    </PaneFrame>
  );
}
