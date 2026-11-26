'use client';

import { useEffect, useState } from 'react';
import { ApiRoute } from '@runkora/lib/routing';
import type { RunsPageResponse, RunWithAttribution } from '@runkora/shared/api';
import { fetchRead } from '@runkora/app/(dashboard)/agents/_services/agents-api';

/** The viewer's own runs, newest first. */
export interface OwnRuns {
  runs: RunWithAttribution[];
  error: string | null;
  loaded: boolean;
}

/** The 30 newest runs the viewer started, read once on mount. */
export function useOwnRuns(): OwnRuns {
  const [own, setOwn] = useState<OwnRuns>({ runs: [], error: null, loaded: false });
  useEffect(() => {
    let live = true;
    void fetchRead<RunsPageResponse>(`${ApiRoute.runs.list()}?created_by=me&limit=30`).then(
      ({ data, error }) => {
        if (live) setOwn({ runs: data?.data ?? [], error, loaded: true });
      },
    );
    return () => {
      live = false;
    };
  }, []);
  return own;
}
