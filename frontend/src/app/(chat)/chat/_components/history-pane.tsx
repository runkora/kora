'use client';

/**
 * @tier 3 — chat-scoped. The chat page's left pane: Home, a new run, then the runs the viewer
 * started, newest first. The open run shows its subagents beneath it, as the sessions pane does.
 */

import Link from 'next/link';
import { Icon, Stack, Text } from '@runkora/hades';
import { APP_ROUTES } from '@runkora/lib/routing';
import { earlierRunsOf } from '@runkora/app/(chat)/agents/[slug]/runs/[runId]/_lib/to-run-view';
import {
  commandParts,
  formatSpan,
  type Row,
  type RunView,
} from '@runkora/app/(chat)/agents/[slug]/runs/[runId]/_lib/run-view';
import {
  PaneFrame,
  SessionRow,
} from '@runkora/app/(chat)/agents/[slug]/runs/[runId]/_components/sessions-pane';
import type { OwnRuns } from '../_hooks/use-own-runs';

/** The run open beside the pane, with its rows for the subagents. */
export interface OpenRun {
  run: RunView;
  rows: Row[];
}

export function HistoryPane({
  own,
  open,
  drawer,
}: {
  own: OwnRuns;
  open?: OpenRun;
  drawer?: boolean;
}) {
  const rest = own.runs.filter((r) => r.id !== open?.run.id);
  const others = earlierRunsOf(rest);
  const subagents = (open?.rows ?? []).flatMap((r) =>
    r.type === 'step' && r.step.kind === 'subagent' ? [r.step] : [],
  );
  return (
    <PaneFrame drawer={drawer}>
      <Stack asChild direction="horizontal" align="center" gap="snug">
        <Link
          href={APP_ROUTES.home}
          style={{ padding: '0 10px', textDecoration: 'none', color: 'inherit' }}
        >
          <Icon name="arrow-left" size="2xs" color="subtle" label="" />
          <Text size="supporting" color="muted">
            Home
          </Text>
        </Link>
      </Stack>
      <SessionRow
        icon="edit"
        label="New run"
        meta=""
        href={open ? APP_ROUTES.chat : undefined}
        active={!open}
      />
      <Stack gap="tight">
        <Text size="caption" color="subtle" style={{ padding: '0 10px 6px' }}>
          Your runs
        </Text>
        {own.error && (
          <Text size="caption" color="status-error" style={{ padding: '0 10px' }}>
            {own.error}
          </Text>
        )}
        {open && (
          <>
            <SessionRow
              label={open.run.summary}
              meta={formatSpan(open.run.durationMs)}
              active
              running={open.run.state === 'active'}
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
          </>
        )}
        {others.map((r, i) => {
          const slug = rest[i]?.agent_slug;
          return (
            <SessionRow
              key={r.id}
              label={r.label}
              meta={r.meta}
              running={r.running}
              href={slug ? APP_ROUTES.agents.chatRun(slug, r.id) : undefined}
            />
          );
        })}
        {!open && others.length === 0 && own.loaded && !own.error && (
          <Text size="caption" color="subtle" style={{ padding: '0 10px' }}>
            Runs you start appear here.
          </Text>
        )}
      </Stack>
    </PaneFrame>
  );
}
