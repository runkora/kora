'use client';

import { useEffect, useRef, useState } from 'react';
import { fetchPeriodRead } from '@runkora/app/(dashboard)/agents/_services/agents-api';
import { useAgentGraders } from '@runkora/app/(dashboard)/agents/[slug]/_hooks/use-agent-graders';
import { ApiRoute } from '@runkora/lib/routing';
import type { RunGrade, RunGradesResponse } from '@runkora/shared/api';

/** Reads of a run that ended on the page, and the gap between them: two minutes in all. */
const RETRIES = 24;
const RETRY_MS = 5_000;

/** What the rail shows: an ended run's grades, or the names of the Graders a live run waits on. */
export type RunGrades = { ended: true; grades: RunGrade[] } | { ended: false; graders: string[] };

/**
 * The run's grades once it has ended. Before then the API has none, so the rail names the agent's
 * Graders instead. A failed read shows nothing.
 */
export function useRunGrades(
  runId: string,
  agentTypeId: string | undefined,
  ended: boolean,
): RunGrades | null {
  const [read, setRead] = useState<{ runId: string; grades: RunGrade[] } | null>(null);
  // Set once the page has seen the run live; grading finishes after the run ends.
  const sawLive = useRef(false);
  useEffect(() => {
    if (!ended) sawLive.current = true;
  }, [ended]);
  useEffect(() => {
    if (!ended) return;
    let current = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let tries = 0;
    const load = () =>
      void fetchPeriodRead<RunGradesResponse>(ApiRoute.runs.grades(runId)).then(({ data }) => {
        if (!current) return;
        if (data) setRead({ runId, grades: data.grades });
        // A run that ended on this page reads again until its first grade lands.
        if (sawLive.current && !data?.grades.length && ++tries < RETRIES)
          timer = setTimeout(load, RETRY_MS);
      });
    load();
    return () => {
      current = false;
      clearTimeout(timer);
    };
  }, [runId, ended]);
  const graders = useAgentGraders(ended ? undefined : agentTypeId);
  if (ended) return read?.runId === runId ? { ended: true, grades: read.grades } : null;
  return graders.data ? { ended: false, graders: graders.data.map((g) => g.name) } : null;
}
