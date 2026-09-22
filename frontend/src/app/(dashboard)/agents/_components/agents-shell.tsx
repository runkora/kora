'use client';

/** @tier 3 — agents-scoped. Client boundary that owns the AgentsProvider. */

import { AgentsProvider } from '../_context/agents-context';
import { AgentsPage } from './agents-page';
import type { AgentTypeRow } from '../_types';
import type { RunStats } from '@orbit/shared/api';

interface AgentsShellProps {
  initialAgentTypes: AgentTypeRow[];
  initialRunStats: RunStats;
  initialError: string | null;
}

export function AgentsShell({
  initialAgentTypes,
  initialRunStats,
  initialError,
}: AgentsShellProps) {
  return (
    <AgentsProvider
      initialAgentTypes={initialAgentTypes}
      initialRunStats={initialRunStats}
      initialError={initialError}
    >
      <AgentsPage />
    </AgentsProvider>
  );
}
