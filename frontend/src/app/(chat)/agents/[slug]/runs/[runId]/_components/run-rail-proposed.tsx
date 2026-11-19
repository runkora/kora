'use client';

// Design mock for the Grader feature. The real implementation later in this stack replaces this file.

/** @tier 3 — run-page-scoped. Proposed: the run rail with a Grades section under Spend (R1). */

import { Stack } from '@runkora/hades';
import type { Grade } from '@runkora/app/(dashboard)/agents/_lib/grades-proposed';
import type { RunView } from '../_lib/run-view';
import { LINE } from '../_lib/run-styles';
import { Links, PeopleStack, RunsOn, Spend } from './run-rail';
import { RailGrades } from './run-grades-proposed';

export function RunRailProposed({
  run,
  sheet,
  grades,
}: {
  run: RunView;
  sheet?: boolean;
  grades: Grade[];
}) {
  return (
    <Stack
      gap="loose"
      shrink={!sheet}
      overflowY="auto"
      style={{
        ...(sheet ? { flex: '1 1 0', minHeight: 0 } : {}),
        width: sheet ? '100%' : 320,
        padding: sheet ? '8px 20px 28px' : '24px 22px',
        borderLeft: sheet ? undefined : LINE,
        scrollbarWidth: 'none',
      }}
    >
      <PeopleStack run={run} />
      <Links run={run} />
      <RunsOn run={run} />
      <Spend run={run} />
      <RailGrades grades={grades} />
    </Stack>
  );
}
