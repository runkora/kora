'use client';

// ── useAgentDetail ────────────────────────────────────────────────────────────
//
// Owns all reactive state for the /agents/[slug] detail page.

import { useState, useCallback, useEffect, useRef } from 'react';
import {
  fetchAgentRuns,
  fetchAgentDetail,
  deleteAgentType,
  deleteRun as apiDeleteRun,
} from '../_services/agent-detail-api';
import { fetchRunStats, fetchStartRun } from '../../_services/agents-api';
import { RUNS_PAGE_SIZE, runStatsBucket } from '../../_types';
import type { AgentTypeRow, RunRow } from '../../_types';
import type { RunStats } from '@orbit/shared/api';

export type DetailStatus = 'idle' | 'loading' | 'success' | 'error' | 'not_found';

export interface AgentDetailStateShape {
  agent: AgentTypeRow | null;
  /** Accumulated pages of runs, newest first. The overview tab's "5 most
   * recent" is this array's first 5 entries — always correct because the
   * first page is fetched newest-first and RUNS_PAGE_SIZE exceeds 5. */
  runs: RunRow[];
  hasMoreRuns: boolean;
  loadingMoreRuns: boolean;
  /** Error from the most recent loadMoreRuns() call, or null. Cleared on the
   * next attempt (success or failure) so a retry doesn't show a stale message. */
  loadMoreRunsError: string | null;
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
  const [loadMoreRunsError, setLoadMoreRunsError] = useState<string | null>(null);
  const [runStats, setRunStats] = useState<RunStats>(initialRunStats);
  const [status, setStatus] = useState<DetailStatus>(initialStatus);
  const [error, setError] = useState<string | null>(initialError);
  const nextCursorRef = useRef<string | null>(initialNextCursor);
  // Guards loadMoreRuns synchronously: loadingMoreRuns is state, so a second
  // click before the re-render would otherwise fetch the same page twice.
  const loadingMoreRef = useRef(false);

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
      fetchRunStats(agent.id),
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
    if (!agent || !nextCursorRef.current || loadingMoreRef.current) return;
    loadingMoreRef.current = true;
    setLoadingMoreRuns(true);
    setLoadMoreRunsError(null);
    const result = await fetchAgentRuns(agent.id, agent, {
      cursor: nextCursorRef.current,
      limit: RUNS_PAGE_SIZE,
    });
    if (result.error) {
      // Leave hasMoreRuns/nextCursorRef untouched so the button stays visible
      // and a retry re-requests the same page instead of skipping it.
      setLoadMoreRunsError(result.error);
    } else {
      setRuns((prev) => [...prev, ...result.data]);
      setHasMoreRuns(result.hasMore);
      nextCursorRef.current = result.nextCursor;
    }
    loadingMoreRef.current = false;
    setLoadingMoreRuns(false);
  }, [agent]);

  const deleteAgent = useCallback(async (): Promise<boolean> => {
    if (!agent) return false;
    const result = await deleteAgentType(agent.id);
    return result.error === null;
  }, [agent]);

  const deleteRun = useCallback(
    async (runId: string): Promise<{ error: string | null }> => {
      const result = await apiDeleteRun(runId);
      if (!result.error) {
        const deleted = runs.find((r) => r.id === runId);
        const bucket = deleted ? runStatsBucket(deleted.state) : null;
        setRuns((prev) => prev.filter((r) => r.id !== runId));
        setRunStats((prev) => ({
          ...prev,
          total: Math.max(0, prev.total - 1),
          ...(bucket ? { [bucket]: Math.max(0, prev[bucket] - 1) } : {}),
        }));
      }
      return result;
    },
    [runs],
  );

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
    loadMoreRunsError,
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
