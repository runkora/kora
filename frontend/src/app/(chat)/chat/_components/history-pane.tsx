'use client';

/**
 * @tier 3 — chat-scoped. The chat page's left pane: Home, a new run, then the runs the viewer
 * started, newest first. The open run shows its subagents beneath it, as the sessions pane does.
 */

import { Stack, Text } from '@runkora/hades';
import { APP_ROUTES } from '@runkora/lib/routing';
import { earlierRunsOf } from '@runkora/app/(chat)/agents/[slug]/runs/[runId]/_lib/to-run-view';
import {
  ENDED,
  type Row,
  type RunView,
} from '@runkora/app/(chat)/agents/[slug]/runs/[runId]/_lib/run-view';
import {
  BackLink,
  PaneFrame,
  RunSessionRows,
  SessionRow,
} from '@runkora/app/(chat)/agents/[slug]/runs/[runId]/_components/sessions-pane';
import type { OwnRuns } from '../_hooks/use-own-runs';

/** The run open beside the pane, with its rows for the subagents. */
interface OpenRun {
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
  const others = own.runs
    .filter((r) => r.id !== open?.run.id)
    .map((r) => ({ ...earlierRunsOf([r])[0]!, slug: r.agent_slug }));
  return (
    <PaneFrame drawer={drawer}>
      <BackLink href={APP_ROUTES.home} label="Home" />
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
          // A run just started is pending or starting, which reads as working, not done.
          <RunSessionRows
            label={open.run.summary}
            run={open.run}
            rows={open.rows}
            running={!ENDED.has(open.run.state ?? 'pending')}
          />
        )}
        {others.map((r) => (
          <SessionRow
            key={r.id}
            label={r.label}
            meta={r.meta}
            running={r.running}
            href={r.slug ? APP_ROUTES.agents.chatRun(r.slug, r.id) : undefined}
          />
        ))}
        {!open && others.length === 0 && own.loaded && !own.error && (
          <Text size="caption" color="subtle" style={{ padding: '0 10px' }}>
            Runs you start appear here.
          </Text>
        )}
      </Stack>
    </PaneFrame>
  );
}
