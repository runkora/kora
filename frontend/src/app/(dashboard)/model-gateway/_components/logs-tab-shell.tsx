'use client';

// ── LogsTabShell ──────────────────────────────────────────────────────────────
//
// Client boundary for the logs tab. Owns the LogsProvider so logs state is
// scoped to this tab only.
//
// `initialRecords`, `initialPats`, `initialHasNextPage`, and `initialNextCursor`
// are server-fetched by the RSC page and passed down here, so the tab renders
// immediately without a client-side loading flash.

import { LogsProvider } from '../_context/logs-context';
import { LogsTab } from './logs-tab';
import type { InferenceUsageRecord } from '../_types';
import type { PAT } from '@orbit/shared/api';

interface LogsTabShellProps {
  initialRecords: InferenceUsageRecord[];
  initialHasNextPage: boolean;
  initialNextCursor: string | null;
  initialPats: PAT[];
  initialError: string | null;
  selectedKeyId?: string;
}

export function LogsTabShell({
  initialRecords,
  initialHasNextPage,
  initialNextCursor,
  initialPats,
  initialError,
  selectedKeyId,
}: LogsTabShellProps) {
  return (
    <LogsProvider
      initialRecords={initialRecords}
      initialHasNextPage={initialHasNextPage}
      initialNextCursor={initialNextCursor}
      initialPats={initialPats}
      initialError={initialError}
    >
      <LogsTab selectedKeyId={selectedKeyId} />
    </LogsProvider>
  );
}
