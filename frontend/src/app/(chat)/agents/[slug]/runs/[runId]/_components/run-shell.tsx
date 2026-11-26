'use client';

/** @tier 3 — run-page-scoped. Client boundary: mounts the run's context and draws the page. */

import { ChatRunRoute } from '@runkora/app/(chat)/chat/_components/chat-run';
import { RunProvider } from '../_context/run-context';
import { RunPage } from './run-page';

/** `fromChat` marks a run opened from the chat page: its left pane lists the viewer's own runs. */
export function RunShell({
  slug,
  runId,
  fromChat,
}: {
  slug: string;
  runId: string;
  fromChat?: boolean;
}) {
  return (
    <RunProvider slug={slug} runId={runId}>
      {fromChat ? <ChatRunRoute runId={runId} /> : <RunPage />}
    </RunProvider>
  );
}
