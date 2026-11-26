'use client';

import { useEffect, useState } from 'react';
import type { RunWithAttribution } from '@runkora/shared/api';
import { fetchRunsPage } from '@runkora/app/(dashboard)/runs/_services/runs-api';

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
    void fetchRunsPage(new URLSearchParams({ created_by: 'me', limit: '30' })).then(
      ({ page, error }) => {
        if (live) setOwn({ runs: page?.runs ?? [], error, loaded: true });
      },
    );
    return () => {
      live = false;
    };
  }, []);
  return own;
}
