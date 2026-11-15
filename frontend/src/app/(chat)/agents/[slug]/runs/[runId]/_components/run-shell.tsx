'use client';

/** @tier 3 — run-page-scoped. Client boundary: mounts the run's context and draws the page. */

import { RunProvider } from '../_context/run-context';
import { RunPage } from './run-page';

export function RunShell({ slug, runId }: { slug: string; runId: string }) {
  return (
    <RunProvider slug={slug} runId={runId}>
      <RunPage />
    </RunProvider>
  );
}
