'use client';

import { useCallback, useEffect, useState } from 'react';
import type { GraderDetail, GraderSummary } from '@runkora/shared/api';
import { fetchAgentGrader, fetchAgentGraders } from '../_services/agent-detail-api';

/** A read's result: `data` is null while loading and on error. */
export interface Read<T> {
  data: T | null;
  error: string | null;
  loading: boolean;
}

/** Reads `load` while `key` is set, again when `key` changes, keeping only the latest answer. */
function useRead<T>(
  key: string | null,
  load: () => Promise<{ data: T | null; error: string | null }>,
): Read<T> {
  const [read, setRead] = useState<Read<T> & { key: string | null }>({
    key: null,
    data: null,
    error: null,
    loading: true,
  });
  useEffect(() => {
    if (!key) return;
    let current = true;
    void load().then(({ data, error }) => {
      if (current) setRead({ key, data, error, loading: false });
    });
    return () => {
      current = false;
    };
  }, [key, load]);
  return read.key === key ? read : { data: null, error: null, loading: true };
}

/** The agent's Graders, failing first. */
export function useAgentGraders(agentTypeId: string | undefined): Read<GraderSummary[]> {
  const load = useCallback(() => fetchAgentGraders(agentTypeId ?? ''), [agentTypeId]);
  const read = useRead(agentTypeId ?? null, load);
  return { ...read, data: read.data ? read.data.graders : null };
}

/** One Grader with its spec and its questions' failed and uncertain runs. */
export function useAgentGrader(agentTypeId: string | undefined, name: string): Read<GraderDetail> {
  const load = useCallback(() => fetchAgentGrader(agentTypeId ?? '', name), [agentTypeId, name]);
  return useRead(agentTypeId ? `${agentTypeId}/${name}` : null, load);
}
