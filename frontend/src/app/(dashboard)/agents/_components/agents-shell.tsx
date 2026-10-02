'use client';

/** @tier 3 — agents-scoped. Client boundary that owns the AgentsProvider. */

import { AgentsProvider } from '../_context/agents-context';
import { AgentsPage } from './agents-page';
import type { AgentTypeRow, RunRow } from '../_types';
import type { AgentsInsights } from '../_lib/spend';

interface AgentsShellProps {
  initialAgentTypes: AgentTypeRow[];
  initialRuns: RunRow[];
  initialError: string | null;
  /** Data the page cannot fetch yet. Stories supply it; the live page passes none. */
  insights?: AgentsInsights;
}

export function AgentsShell({
  initialAgentTypes,
  initialRuns,
  initialError,
  insights,
}: AgentsShellProps) {
  return (
    <AgentsProvider
      initialAgentTypes={initialAgentTypes}
      initialRuns={initialRuns}
      initialError={initialError}
    >
      <AgentsPage {...(insights ? { insights } : {})} />
    </AgentsProvider>
  );
}
