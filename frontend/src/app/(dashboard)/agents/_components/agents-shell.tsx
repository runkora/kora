'use client';

/** @tier 3 — agents-scoped. Client boundary that owns the AgentsProvider. */

import { AgentsProvider } from '../_context/agents-context';
import { AgentsPage } from './agents-page';
import type { AgentInsightsResponse, AgentRunStats } from '@runkora/shared/api';
import type { AgentTypeRow, RunRow } from '../_types';

interface AgentsShellProps {
  initialAgentTypes: AgentTypeRow[];
  initialLiveRuns: RunRow[];
  initialRunStats: AgentRunStats | null;
  initialError: string | null;
  /** Why the runs could not be read, when the agents could. */
  initialRunsError?: string | null;
  /** The 7-day insights the server render fetched. */
  initialInsights?: AgentInsightsResponse | null;
}

export function AgentsShell({
  initialAgentTypes,
  initialLiveRuns,
  initialRunStats,
  initialError,
  initialRunsError = null,
  initialInsights,
}: AgentsShellProps) {
  return (
    <AgentsProvider
      initialAgentTypes={initialAgentTypes}
      initialLiveRuns={initialLiveRuns}
      initialRunStats={initialRunStats}
      initialError={initialError}
      initialRunsError={initialRunsError}
    >
      <AgentsPage {...(initialInsights ? { initialInsights } : {})} />
    </AgentsProvider>
  );
}
