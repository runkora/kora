'use client';

// ── useAgents ─────────────────────────────────────────────────────────────────
//
// Owns all reactive state for the /agents list page. Accepts initial server-
// rendered data so the page renders immediately without a loading flash.
//
// FeatureStateShape upgrade contract: when this feature graduates to Zustand,
// the store's state + actions must satisfy the AgentsStateShape interface.

import { useState, useCallback } from 'react';
import { fetchAgentTypes, fetchRunStats } from '../_services/agents-api';
import type { AgentTypeRow } from '../_types';
import type { RunStats } from '@orbit/shared/api';

export type LoadStatus = 'idle' | 'loading' | 'success' | 'error';

const emptyRunStats: RunStats = {
  total: 0,
  active: 0,
  completed: 0,
  failed: 0,
  avg_completed_duration_ms: null,
};

export interface AgentsStateShape {
  agentTypes: AgentTypeRow[];
  runStats: RunStats;
  status: LoadStatus;
  error: string | null;
  refresh: () => Promise<void>;
}

export function useAgents(
  initialAgentTypes: AgentTypeRow[],
  initialRunStats: RunStats,
  initialError: string | null,
): AgentsStateShape {
  const [agentTypes, setAgentTypes] = useState<AgentTypeRow[]>(initialAgentTypes);
  const [runStats, setRunStats] = useState<RunStats>(initialRunStats);
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

    const statsResult = await fetchRunStats();
    if (!statsResult.error && statsResult.data) {
      setRunStats(statsResult.data);
    } else {
      setRunStats(emptyRunStats);
    }

    setStatus('success');
  }, []);

  return { agentTypes, runStats, status, error, refresh };
}
