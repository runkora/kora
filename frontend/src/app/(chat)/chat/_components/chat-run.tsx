'use client';

/** @tier 3 — chat-scoped. The run page as the chat page shows it: the viewer's runs in the left pane. */

import { useEffect, useState } from 'react';
import {
  RunPage,
  type RunPageEnter,
} from '@runkora/app/(chat)/agents/[slug]/runs/[runId]/_components/run-page';
import { useOwnRuns, type OwnRuns } from '../_hooks/use-own-runs';
import { forgetHandOver, handedOver } from '../_lib/hand-over';
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

/**
 * A run at its own address, opened from the chat page. A run the chat page just started arrives
 * with the composer's box and the runs that page loaded, which show until its own read returns.
 */
export function ChatRunRoute({ runId }: { runId: string }) {
  const [handed] = useState(() => handedOver(runId));
  useEffect(() => {
    forgetHandOver(runId);
  }, [runId]);
  const own = useOwnRuns();
  return <ChatRun own={own.loaded || !handed ? own : handed.own} enter={handed?.enter} />;
}
