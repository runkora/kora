'use client';

import { useCallback, useEffect, useState } from 'react';
import type { GraderDetail, GraderSummary } from '@runkora/shared/api';
import { fetchAgentGrader, fetchAgentGraders } from '../_services/agent-detail-api';

/** A read's result: `data` is null while loading and on error. */
interface Read<T> {
  data: T | null;
  error: string | null;
  loading: boolean;
  /** Reads again, for a retry after an error. */
  reload: () => void;
}

/** Reads `load` while `key` is set, again when `key` changes or on reload, keeping only the latest answer. */
function useRead<T>(
  key: string | null,
  load: () => Promise<{ data: T | null; error: string | null }>,
): Read<T> {
  // Bumped by reload; a new attempt is a new read key, so it shows as loading.
  const [attempt, setAttempt] = useState(0);
  const reload = useCallback(() => setAttempt((n) => n + 1), []);
  const readKey = key === null ? null : `${attempt}:${key}`;
  const [read, setRead] = useState<Omit<Read<T>, 'reload'> & { key: string | null }>({
    key: null,
    data: null,
    error: null,
    loading: true,
  });
  useEffect(() => {
    if (!readKey) return;
    let current = true;
    void load().then(({ data, error }) => {
      if (current) setRead({ key: readKey, data, error, loading: false });
    });
    return () => {
      current = false;
    };
  }, [readKey, load]);
  return read.key === readKey
    ? { data: read.data, error: read.error, loading: read.loading, reload }
    : { data: null, error: null, loading: true, reload };
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
