'use client';

// ── useAgentDetail ────────────────────────────────────────────────────────────
//
// Owns all reactive state for the /agents/[slug] detail page.

import { useState, useCallback, useEffect, useRef } from 'react';
import {
  fetchAgentRuns,
  fetchAgentRunStats,
  fetchAgentDetail,
  deleteAgentType,
  deleteRun as apiDeleteRun,
} from '../_services/agent-detail-api';
import { fetchStartRun } from '../../_services/agents-api';
import type { AgentTypeRow, RunRow } from '../../_types';
import type { RunStats } from '@orbit/shared/api';

export type DetailStatus = 'idle' | 'loading' | 'success' | 'error' | 'not_found';

/** Page size for the runs tab's "load more" pagination. */
const RUNS_PAGE_SIZE = 25;

export interface AgentDetailStateShape {
  agent: AgentTypeRow | null;
  /** Accumulated pages of runs, newest first. The overview tab's "5 most
   * recent" is this array's first 5 entries — always correct because the
   * first page is fetched newest-first and RUNS_PAGE_SIZE exceeds 5. */
  runs: RunRow[];
  hasMoreRuns: boolean;
  loadingMoreRuns: boolean;
  loadMoreRuns: () => Promise<void>;
  runStats: RunStats;
  status: DetailStatus;
  error: string | null;
  refresh: () => Promise<void>;
  deleteAgent: () => Promise<boolean>;
  deleteRun: (runId: string) => Promise<{ error: string | null }>;
  startRun: (
    task: string,
    provisionSandbox: boolean,
  ) => Promise<{ runId: string | null; error: string | null }>;
}

export function useAgentDetail(
  initialAgent: AgentTypeRow | null,
  initialRuns: RunRow[],
  initialHasMoreRuns: boolean,
  initialNextCursor: string | null,
  initialRunStats: RunStats,
  initialError: string | null,
  initialStatus: DetailStatus,
): AgentDetailStateShape {
  const [agent, setAgent] = useState<AgentTypeRow | null>(initialAgent);
  const [runs, setRuns] = useState<RunRow[]>(initialRuns);
  const [hasMoreRuns, setHasMoreRuns] = useState(initialHasMoreRuns);
  const [loadingMoreRuns, setLoadingMoreRuns] = useState(false);
  const [runStats, setRunStats] = useState<RunStats>(initialRunStats);
  const [status, setStatus] = useState<DetailStatus>(initialStatus);
  const [error, setError] = useState<string | null>(initialError);
  const nextCursorRef = useRef<string | null>(initialNextCursor);

  // Poll for state transitions while the agent type is provisioning.
  const pollingRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const agentId = agent?.id;
  const agentState = agent?.state;
  useEffect(() => {
    if (!agentId || agentState !== 'provisioning') return;

    pollingRef.current = setInterval(() => {
      void fetchAgentDetail(agentId).then((result) => {
        if (result.agentType && result.agentType.state !== 'provisioning') {
          setAgent(result.agentType);
          if (pollingRef.current) clearInterval(pollingRef.current);
        }
      });
    }, 3000);

    return () => {
      if (pollingRef.current) clearInterval(pollingRef.current);
    };
  }, [agentId, agentState]);

  const refresh = useCallback(async () => {
    if (!agent) return;
    setStatus('loading');
    setError(null);

    const [runsResult, statsResult] = await Promise.all([
      fetchAgentRuns(agent.id, agent, { limit: RUNS_PAGE_SIZE }),
      fetchAgentRunStats(agent.id),
    ]);
    if (runsResult.error) {
      setError(runsResult.error);
      setStatus('error');
      return;
    }
    setRuns(runsResult.data);
    setHasMoreRuns(runsResult.hasMore);
    nextCursorRef.current = runsResult.nextCursor;
    if (statsResult.data) {
      setRunStats(statsResult.data);
    }
    setStatus('success');
  }, [agent]);

  const loadMoreRuns = useCallback(async () => {
    if (!agent || !nextCursorRef.current || loadingMoreRuns) return;
    setLoadingMoreRuns(true);
    const result = await fetchAgentRuns(agent.id, agent, {
      cursor: nextCursorRef.current,
      limit: RUNS_PAGE_SIZE,
    });
    if (!result.error) {
      setRuns((prev) => [...prev, ...result.data]);
      setHasMoreRuns(result.hasMore);
      nextCursorRef.current = result.nextCursor;
    }
    setLoadingMoreRuns(false);
  }, [agent, loadingMoreRuns]);

  const deleteAgent = useCallback(async (): Promise<boolean> => {
    if (!agent) return false;
    const result = await deleteAgentType(agent.id);
    return result.error === null;
  }, [agent]);

  const deleteRun = useCallback(async (runId: string): Promise<{ error: string | null }> => {
    const result = await apiDeleteRun(runId);
    if (!result.error) {
      setRuns((prev) => prev.filter((r) => r.id !== runId));
      setRunStats((prev) => ({ ...prev, total: Math.max(0, prev.total - 1) }));
    }
    return result;
  }, []);

  const startRun = useCallback(
    async (
      task: string,
      provisionSandbox: boolean,
    ): Promise<{ runId: string | null; error: string | null }> => {
      if (!agent) return { runId: null, error: 'No agent loaded.' };
      const result = await fetchStartRun(agent.id, { task, provision_sandbox: provisionSandbox });
      if (result.error) return { runId: null, error: result.error };
      return { runId: result.run?.id ?? null, error: null };
    },
    [agent],
  );

  return {
    agent,
    runs,
    hasMoreRuns,
    loadingMoreRuns,
    loadMoreRuns,
    runStats,
    status,
    error,
    refresh,
    deleteAgent,
    deleteRun,
    startRun,
  };
}
