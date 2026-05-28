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
import { fetchStartRun } from '../../_services/agents-api';
import type { AgentTypeRow, RunRow } from '../../_types';

export type DetailStatus = 'idle' | 'loading' | 'success' | 'error' | 'not_found';

export interface AgentDetailStateShape {
  agent: AgentTypeRow | null;
  runs: RunRow[];
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
  initialError: string | null,
  initialStatus: DetailStatus,
): AgentDetailStateShape {
  const [agent, setAgent] = useState<AgentTypeRow | null>(initialAgent);
  const [runs, setRuns] = useState<RunRow[]>(initialRuns);
  const [status, setStatus] = useState<DetailStatus>(initialStatus);
  const [error, setError] = useState<string | null>(initialError);

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

    const result = await fetchAgentRuns(agent.id, agent);
    if (result.error) {
      setError(result.error);
      setStatus('error');
    } else {
      setRuns(result.data);
      setStatus('success');
    }
  }, [agent]);

  const deleteAgent = useCallback(async (): Promise<boolean> => {
    if (!agent) return false;
    const result = await deleteAgentType(agent.id);
    return result.error === null;
  }, [agent]);

  const deleteRun = useCallback(async (runId: string): Promise<{ error: string | null }> => {
    const result = await apiDeleteRun(runId);
    if (!result.error) {
      setRuns((prev) => prev.filter((r) => r.id !== runId));
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

  return { agent, runs, status, error, refresh, deleteAgent, deleteRun, startRun };
}
