'use client';

/** @tier 3 — agents-scoped. Client boundary that owns the AgentsProvider. */

import { AgentsProvider } from '../_context/agents-context';
import { AgentsPage } from './agents-page';
import type { AgentInsightsResponse } from '@orbit/shared/api';
import type { AgentTypeRow, RunRow } from '../_types';
import type { AgentsInsights } from '../_lib/spend';

interface AgentsShellProps {
  initialAgentTypes: AgentTypeRow[];
  initialRuns: RunRow[];
  initialError: string | null;
  /** The page's optional parts, handed in directly. Stories supply it; the live page loads its own. */
  insights?: AgentsInsights;
  /** The 7-day insights the server render fetched. */
  initialInsights?: AgentInsightsResponse | null;
}

export function AgentsShell({
  initialAgentTypes,
  initialRuns,
  initialError,
  insights,
  initialInsights,
}: AgentsShellProps) {
  return (
    <AgentsProvider
      initialAgentTypes={initialAgentTypes}
      initialRuns={initialRuns}
      initialError={initialError}
    >
      <AgentsPage
        {...(insights ? { insights } : {})}
        {...(initialInsights ? { initialInsights } : {})}
      />
    </AgentsProvider>
  );
}
