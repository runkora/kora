'use client';

// ── useAgentDetail ────────────────────────────────────────────────────────────
//
// Owns all reactive state for the /agents/[slug] detail page.

import { useState, useCallback, useEffect, useRef } from 'react';
import {
  fetchAgentOverview,
  fetchAgentRuns,
  fetchAgentDetail,
  deleteAgentType,
  deleteRun as apiDeleteRun,
} from '../_services/agent-detail-api';
import { fetchStartRun } from '../../_services/agents-api';
import type { RunStateCounts } from '@orbit/shared/api';
import { DEFAULT_PERIOD } from '../../_lib/spend';
import { RUNS_PAGE_SIZE } from '../../_types';
import type { AgentTypeRow, RunRow, RunState } from '../../_types';

export type DetailStatus = 'idle' | 'loading' | 'success' | 'error' | 'not_found';

/** The Runs tab's status filters and the run states each one reads. */
export type RunFilter = 'all' | 'active' | 'completed' | 'failed';
export const RUN_FILTER_STATES: Record<RunFilter, readonly RunState[]> = {
  all: [],
  active: ['pending', 'starting', 'active', 'idle', 'completing'],
  completed: ['completed'],
  failed: ['failed', 'budget_exhausted'],
};

export interface AgentDetailStateShape {
  agent: AgentTypeRow | null;
  /** The pages of runs loaded so far under `runFilter`, newest first. */
  runs: RunList;
  /** The newest runs whatever the filter, for the Overview. */
  recentRuns: RunRow[];
  /** Every run of the agent counted by state; null when the counts could not be read. */
  stateCounts: RunStateCounts | null;
  runFilter: RunFilter;
  /** Switches the filter and loads its first page. */
  setRunFilter: (filter: RunFilter) => void;
  /** Appends the next page of runs. */
  loadMoreRuns: () => Promise<void>;
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

/** Loaded runs and where the next page starts. */
export interface RunList {
  rows: RunRow[];
  /** The next page's cursor; null when every run is loaded. */
  nextCursor: string | null;
  loadingMore: boolean;
  /** Why the last load-more failed; cleared by the next attempt. */
  loadMoreError: string | null;
}

export function useAgentDetail(
  initialAgent: AgentTypeRow | null,
  initialRuns: RunRow[],
  initialNextCursor: string | null,
  initialStateCounts: RunStateCounts | null,
  initialError: string | null,
  initialStatus: DetailStatus,
): AgentDetailStateShape {
  const [agent, setAgent] = useState<AgentTypeRow | null>(initialAgent);
  const [runs, setRuns] = useState<RunList>({
    rows: initialRuns,
    nextCursor: initialNextCursor,
    loadingMore: false,
    loadMoreError: null,
  });
  const [recentRuns, setRecentRuns] = useState<RunRow[]>(initialRuns);
  const [stateCounts, setStateCounts] = useState<RunStateCounts | null>(initialStateCounts);
  const [runFilter, setRunFilterState] = useState<RunFilter>('all');
  const [status, setStatus] = useState<DetailStatus>(initialStatus);
  const [error, setError] = useState<string | null>(initialError);
  // Bumped by every first-page load, so a page requested under an older filter is dropped.
  const generation = useRef(0);
  // The generation a load more is running for. Set synchronously, so a second
  // click before the re-render does not fetch the same page twice.
  const loadingMore = useRef<number | null>(null);

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

  const loadFirstPage = useCallback(
    async (filter: RunFilter) => {
      if (!agent) return;
      const gen = ++generation.current;
      // The previous filter's cursor must not page the new one.
      setRuns((prev) => ({ ...prev, nextCursor: null, loadingMore: false, loadMoreError: null }));
      setStatus('loading');
      setError(null);
      const result = await fetchAgentRuns(agent, {
        states: RUN_FILTER_STATES[filter],
        limit: RUNS_PAGE_SIZE,
      });
      if (gen !== generation.current) return;
      if (result.error) {
        setError(result.error);
        setStatus('error');
        return;
      }
      setRuns({
        rows: result.data,
        nextCursor: result.nextCursor,
        loadingMore: false,
        loadMoreError: null,
      });
      if (filter === 'all') setRecentRuns(result.data);
      setStatus('success');
    },
    [agent],
  );

  // Re-reads the counts after the runs change. A failed read keeps the previous counts.
  const refreshCounts = useCallback(async () => {
    if (!agent) return;
    const { data } = await fetchAgentOverview(agent.slug, DEFAULT_PERIOD);
    if (data?.runStats) setStateCounts(data.runStats.current);
  }, [agent]);

  const refresh = useCallback(async () => {
    await Promise.all([loadFirstPage(runFilter), refreshCounts()]);
  }, [loadFirstPage, refreshCounts, runFilter]);

  const setRunFilter = useCallback(
    (filter: RunFilter) => {
      setRunFilterState(filter);
      void loadFirstPage(filter);
    },
    [loadFirstPage],
  );

  const loadMoreRuns = useCallback(async () => {
    const gen = generation.current;
    if (!agent || !runs.nextCursor || loadingMore.current === gen) return;
    loadingMore.current = gen;
    setRuns((prev) => ({ ...prev, loadingMore: true, loadMoreError: null }));
    const result = await fetchAgentRuns(agent, {
      states: RUN_FILTER_STATES[runFilter],
      limit: RUNS_PAGE_SIZE,
      cursor: runs.nextCursor,
    });
    if (loadingMore.current === gen) loadingMore.current = null;
    if (gen !== generation.current) return;
    setRuns((prev) =>
      result.error
        ? // The cursor stays, so a retry asks for the same page.
          { ...prev, loadingMore: false, loadMoreError: result.error }
        : {
            rows: [...prev.rows, ...result.data],
            nextCursor: result.nextCursor,
            loadingMore: false,
            loadMoreError: null,
          },
    );
  }, [agent, runs.nextCursor, runFilter]);

  const deleteAgent = useCallback(async (): Promise<boolean> => {
    if (!agent) return false;
    const result = await deleteAgentType(agent.id);
    return result.error === null;
  }, [agent]);

  const deleteRun = useCallback(
    async (runId: string): Promise<{ error: string | null }> => {
      const result = await apiDeleteRun(runId);
      if (!result.error) {
        setRuns((prev) => ({ ...prev, rows: prev.rows.filter((r) => r.id !== runId) }));
        setRecentRuns((prev) => prev.filter((r) => r.id !== runId));
        await refreshCounts();
      }
      return result;
    },
    [refreshCounts],
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
    recentRuns,
    stateCounts,
    runFilter,
    setRunFilter,
    loadMoreRuns,
    status,
    error,
    refresh,
    deleteAgent,
    deleteRun,
    startRun,
  };
}
