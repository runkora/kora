'use client';

/** @tier 3 — agents/[slug]-scoped. Client boundary for the agent detail page. */

import { AgentDetailProvider } from '../_context/agent-detail-context';
import { AgentDetailPage } from './agent-detail-page';
import type { DetailStatus } from '../_hooks/use-agent-detail';
import type { AgentTypeRow, RunRow } from '../../_types';
import type { AgentOverviewResponse } from '@orbit/shared/api';

interface AgentDetailShellProps {
  initialAgent: AgentTypeRow | null;
  initialRuns: RunRow[];
  /** Where the second page of runs starts; null when the first page holds them all. */
  initialNextCursor: string | null;
  initialError: string | null;
  initialStatus: DetailStatus;
  /** The default window's overview, fetched by the server render. */
  initialOverview?: AgentOverviewResponse | null;
}

export function AgentDetailShell({
  initialAgent,
  initialRuns,
  initialNextCursor,
  initialError,
  initialStatus,
  initialOverview,
}: AgentDetailShellProps) {
  return (
    <AgentDetailProvider
      initialAgent={initialAgent}
      initialRuns={initialRuns}
      initialNextCursor={initialNextCursor}
      initialStateCounts={initialOverview?.runStats?.current ?? null}
      initialError={initialError}
      initialStatus={initialStatus}
    >
      <AgentDetailPage initialOverview={initialOverview} />
    </AgentDetailProvider>
  );
}
