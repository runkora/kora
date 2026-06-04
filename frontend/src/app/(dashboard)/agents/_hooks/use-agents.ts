'use client';

// ── useAgents ─────────────────────────────────────────────────────────────────
//
// Owns all reactive state for the /agents list page. Accepts initial server-
// rendered data so the page renders immediately without a loading flash.
//
// FeatureStateShape upgrade contract: when this feature graduates to Zustand,
// the store's state + actions must satisfy the AgentsStateShape interface.

import { useState, useCallback } from 'react';
import { fetchAgentTypes, fetchRuns } from '../_services/agents-api';
import type { AgentTypeRow, RunRow } from '../_types';

export type LoadStatus = 'idle' | 'loading' | 'success' | 'error';

export interface AgentsStateShape {
  agentTypes: AgentTypeRow[];
  recentRuns: RunRow[];
  status: LoadStatus;
  error: string | null;
  refresh: () => Promise<void>;
}

export function useAgents(
  initialAgentTypes: AgentTypeRow[],
  initialRuns: RunRow[],
  initialError: string | null,
): AgentsStateShape {
  const [agentTypes, setAgentTypes] = useState<AgentTypeRow[]>(initialAgentTypes);
  const [recentRuns, setRecentRuns] = useState<RunRow[]>(initialRuns);
  const [status, setStatus] = useState<LoadStatus>(
    initialError ? 'error' : initialAgentTypes.length > 0 ? 'success' : 'idle',
  );
  const [error, setError] = useState<string | null>(initialError);

  const refresh = useCallback(async () => {
    setStatus('loading');
    setError(null);

    const typesResult = await fetchAgentTypes();
    if (typesResult.error) {
      setError(typesResult.error);
      setStatus('error');
      return;
    }

    setAgentTypes(typesResult.data);

    const runsResult = await fetchRuns(typesResult.data);
    if (!runsResult.error) {
      setRecentRuns(runsResult.data);
    }

    setStatus('success');
  }, []);

  return { agentTypes, recentRuns, status, error, refresh };
}
