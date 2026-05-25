'use client';

/** @tier 3 — agents-scoped. Client boundary that owns the AgentsProvider. */

import { AgentsProvider } from '../_context/agents-context';
import { AgentsPage } from './agents-page';
import type { AgentTypeRow, RunRow } from '../_types';

interface AgentsShellProps {
  initialAgentTypes: AgentTypeRow[];
  initialRuns: RunRow[];
  initialError: string | null;
}

export function AgentsShell({ initialAgentTypes, initialRuns, initialError }: AgentsShellProps) {
  return (
    <AgentsProvider
      initialAgentTypes={initialAgentTypes}
      initialRuns={initialRuns}
      initialError={initialError}
    >
      <AgentsPage />
    </AgentsProvider>
  );
}
