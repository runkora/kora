'use client';

// ── AgentDetailContext ────────────────────────────────────────────────────────
//
// Provides AgentDetailStateShape to the entire /agents/[slug] tree.

import { createContext, useContext } from 'react';
import { useAgentDetail } from '../_hooks/use-agent-detail';
import type { AgentDetailStateShape, DetailStatus } from '../_hooks/use-agent-detail';
import type { RunStateCounts } from '@orbit/shared/api';
import type { AgentTypeRow, RunRow } from '../../_types';

const AgentDetailContext = createContext<AgentDetailStateShape | null>(null);

interface AgentDetailProviderProps {
  initialAgent: AgentTypeRow | null;
  initialRuns: RunRow[];
  initialNextCursor: string | null;
  initialStateCounts: RunStateCounts | null;
  initialError: string | null;
  initialStatus: DetailStatus;
  children: React.ReactNode;
}

export function AgentDetailProvider({
  initialAgent,
  initialRuns,
  initialNextCursor,
  initialStateCounts,
  initialError,
  initialStatus,
  children,
}: AgentDetailProviderProps) {
  const state = useAgentDetail(
    initialAgent,
    initialRuns,
    initialNextCursor,
    initialStateCounts,
    initialError,
    initialStatus,
  );
  return <AgentDetailContext.Provider value={state}>{children}</AgentDetailContext.Provider>;
}

export function useAgentDetailContext(): AgentDetailStateShape {
  const ctx = useContext(AgentDetailContext);
  if (!ctx) {
    throw new Error('useAgentDetailContext must be used inside <AgentDetailProvider>');
  }
  return ctx;
}
