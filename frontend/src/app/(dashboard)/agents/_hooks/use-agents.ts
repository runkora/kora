'use client';

// ── useAgents ─────────────────────────────────────────────────────────────────
//
// Owns all reactive state for the /agents list page. Accepts initial server-
// rendered data so the page renders immediately without a loading flash.
//
// FeatureStateShape upgrade contract: when this feature graduates to Zustand,
// the store's state + actions must satisfy the AgentsStateShape interface.

import { useState, useCallback } from 'react';
import type { AgentRunStats } from '@runkora/shared/api';
import { fetchAgentTypes, fetchRuns, fetchRunStats } from '../_services/agents-api';
import { DEFAULT_PERIOD, RUNNING_ROWS } from '../_lib/spend';
import { LIVE_RUN_STATES } from '../_types';
import type { AgentTypeRow, RunRow } from '../_types';

export type LoadStatus = 'idle' | 'loading' | 'success' | 'error';

export interface AgentsStateShape {
  agentTypes: AgentTypeRow[];
  /** The newest live runs, at most RUNNING_ROWS; `runStats` counts them all. */
  liveRuns: RunRow[];
  /** Tenant run stats for the default window; null when they could not be read. */
  runStats: AgentRunStats | null;
  status: LoadStatus;
  error: string | null;
  /** Why the runs could not be read. The agents still show. */
  runsError: string | null;
  refresh: () => Promise<void>;
}

export function useAgents(
  initialAgentTypes: AgentTypeRow[],
  initialLiveRuns: RunRow[],
  initialRunStats: AgentRunStats | null,
  initialError: string | null,
  initialRunsError: string | null = null,
): AgentsStateShape {
  const [agentTypes, setAgentTypes] = useState<AgentTypeRow[]>(initialAgentTypes);
  const [liveRuns, setLiveRuns] = useState<RunRow[]>(initialLiveRuns);
  const [runStats, setRunStats] = useState<AgentRunStats | null>(initialRunStats);
  const [status, setStatus] = useState<LoadStatus>(
    initialError ? 'error' : initialAgentTypes.length > 0 ? 'success' : 'idle',
  );
  const [error, setError] = useState<string | null>(initialError);
  const [runsError, setRunsError] = useState<string | null>(initialRunsError);

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

    const [runsResult, statsResult] = await Promise.all([
      fetchRuns(typesResult.data, { states: LIVE_RUN_STATES, limit: RUNNING_ROWS }),
      fetchRunStats(DEFAULT_PERIOD),
    ]);
    // The live runs and their counts update together, so the Running now card never mixes two reads.
    if (!runsResult.error && statsResult.data) {
      setLiveRuns(runsResult.data);
      setRunStats(statsResult.data);
    }
    setRunsError(runsResult.error);

    setStatus('success');
  }, []);

  return { agentTypes, liveRuns, runStats, status, error, runsError, refresh };
}
