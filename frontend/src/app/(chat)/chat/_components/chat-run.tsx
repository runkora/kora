'use client';

/** @tier 3 — chat-scoped. The run page as the chat page shows it: the viewer's runs in the left pane. */

import {
  RunPage,
  type RunPageEnter,
} from '@runkora/app/(chat)/agents/[slug]/runs/[runId]/_components/run-page';
import { useOwnRuns, type OwnRuns } from '../_hooks/use-own-runs';
import { HistoryPane } from './history-pane';

/** Renders inside the run's `RunProvider`. */
export function ChatRun({
  own,
  enter,
  height,
}: {
  own: OwnRuns;
  enter?: RunPageEnter;
  height?: string;
}) {
  return (
    <RunPage
      height={height}
      enter={enter}
      pane={({ run, rows, drawer }) => (
        <HistoryPane own={own} open={{ run, rows }} drawer={drawer} />
      )}
    />
  );
}

/** A run opened at its own address from the chat page, which reads the viewer's runs itself. */
export function ChatRunRoute() {
  return <ChatRun own={useOwnRuns()} />;
}
