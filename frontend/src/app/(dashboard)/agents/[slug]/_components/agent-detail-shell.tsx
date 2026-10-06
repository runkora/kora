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
  initialError: string | null;
  initialStatus: DetailStatus;
  /** The default window's overview, fetched by the server render. */
  initialOverview?: AgentOverviewResponse | null;
}

export function AgentDetailShell({
  initialAgent,
  initialRuns,
  initialError,
  initialStatus,
  initialOverview,
}: AgentDetailShellProps) {
  return (
    <AgentDetailProvider
      initialAgent={initialAgent}
      initialRuns={initialRuns}
      initialError={initialError}
      initialStatus={initialStatus}
    >
      <AgentDetailPage initialOverview={initialOverview} />
    </AgentDetailProvider>
  );
}
