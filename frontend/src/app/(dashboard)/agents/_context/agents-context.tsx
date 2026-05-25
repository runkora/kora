'use client';

// ── AgentsContext ──────────────────────────────────────────────────────────────
//
// Provides AgentsStateShape to any component inside the /agents page tree.
// The RSC page fetches server-side and passes initialData to AgentsProvider,
// which bootstraps useAgents synchronously — no useEffect hydration flash.

import { createContext, useContext } from 'react';
import { useAgents } from '../_hooks/use-agents';
import type { AgentsStateShape } from '../_hooks/use-agents';
import type { AgentTypeRow, RunRow } from '../_types';

const AgentsContext = createContext<AgentsStateShape | null>(null);

interface AgentsProviderProps {
  initialAgentTypes: AgentTypeRow[];
  initialRuns: RunRow[];
  initialError: string | null;
  children: React.ReactNode;
}

export function AgentsProvider({
  initialAgentTypes,
  initialRuns,
  initialError,
  children,
}: AgentsProviderProps) {
  const state = useAgents(initialAgentTypes, initialRuns, initialError);
  return <AgentsContext.Provider value={state}>{children}</AgentsContext.Provider>;
}

export function useAgentsContext(): AgentsStateShape {
  const ctx = useContext(AgentsContext);
  if (!ctx) {
    throw new Error('useAgentsContext must be used inside <AgentsProvider>');
  }
  return ctx;
}
