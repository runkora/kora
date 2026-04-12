'use client';

// ── LogsTabShell ──────────────────────────────────────────────────────────────
//
// Client boundary for the logs tab. Owns the LogsProvider so logs state is
// scoped to this tab only.
//
// `initialRecords`, `initialPats`, and `initialTotal` are server-fetched by the
// RSC page and passed down here, so the tab renders immediately without a
// client-side loading flash.

import { LogsProvider } from '../_context/logs-context';
import { LogsTab } from './logs-tab';
import type { InferenceUsageRecord } from '../_types';
import type { PAT } from '@orbit/shared/api';

interface LogsTabShellProps {
  initialRecords: InferenceUsageRecord[];
  initialTotal: number;
  initialPats: PAT[];
  initialError: string | null;
  selectedKeyId?: string;
}

export function LogsTabShell({
  initialRecords,
  initialTotal,
  initialPats,
  initialError,
  selectedKeyId,
}: LogsTabShellProps) {
  return (
    <LogsProvider
      initialRecords={initialRecords}
      initialTotal={initialTotal}
      initialPats={initialPats}
      initialError={initialError}
    >
      <LogsTab selectedKeyId={selectedKeyId} />
    </LogsProvider>
  );
}
