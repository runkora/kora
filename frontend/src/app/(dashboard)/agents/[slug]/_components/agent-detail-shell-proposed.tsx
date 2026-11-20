'use client';

// Design mock for the Grader feature. The real implementation later in this stack replaces this file.

/** @tier 3 — agents/[slug]-scoped. Proposed: the detail page's client boundary, drawing the page with grades. */

import { AgentDetailProvider } from '../_context/agent-detail-context';
import { AgentDetailPageProposed } from './agent-detail-page-proposed';
import type { GraderSeries } from '../../_lib/grades-proposed';
import type { DetailStatus } from '../_hooks/use-agent-detail';
import type { AgentTypeRow, RunRow } from '../../_types';
import type { AgentOverviewResponse } from '@runkora/shared/api';

interface AgentDetailShellProps {
  initialAgent: AgentTypeRow | null;
  initialRuns: RunRow[];
  /** Where the second page of runs starts; null when the first page holds them all. */
  initialNextCursor: string | null;
  initialError: string | null;
  initialStatus: DetailStatus;
  /** The default window's overview, fetched by the server render. */
  initialOverview?: AgentOverviewResponse | null;
  grades: GraderSeries[];
  initialTab?: string;
  initialGrader?: string;
}

export function AgentDetailShellProposed({
  initialAgent,
  initialRuns,
  initialNextCursor,
  initialError,
  initialStatus,
  initialOverview,
  grades,
  initialTab,
  initialGrader,
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
      <AgentDetailPageProposed
        initialOverview={initialOverview}
        grades={grades}
        initialTab={initialTab}
        initialGrader={initialGrader}
      />
    </AgentDetailProvider>
  );
}
