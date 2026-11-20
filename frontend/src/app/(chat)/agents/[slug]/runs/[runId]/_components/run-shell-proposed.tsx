'use client';

// Design mock for the Grader feature. The real implementation later in this stack replaces this file.

/** @tier 3 — run-page-scoped. Proposed: the run shell drawing the run page with grades. */

import type { Grade } from '@runkora/app/(dashboard)/agents/_lib/grades-proposed';
import { RunProvider } from '../_context/run-context';
import { RunPageProposed } from './run-page-proposed';

export function RunShellProposed({
  slug,
  runId,
  grades,
}: {
  slug: string;
  runId: string;
  grades: Grade[];
}) {
  return (
    <RunProvider slug={slug} runId={runId}>
      <RunPageProposed grades={grades} />
    </RunProvider>
  );
}
