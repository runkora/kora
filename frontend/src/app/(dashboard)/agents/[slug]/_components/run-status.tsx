'use client';

/** @tier 3 · agents/[slug]-scoped. A run's state as the Running now table draws it: a dot and a label. */

import { Stack, StatusDot, Text } from '@orbit/terra';
import type { StatusDotProps } from '@orbit/terra';
import { LIVE_STATES } from '../../_components/running-now-card';
import type { RunRow } from '../../_types';

/** The live states keep the Running now vocabulary; finished runs add their own. */
const RUN_STATES: Record<string, { status: NonNullable<StatusDotProps['status']>; label: string }> =
  {
    ...LIVE_STATES,
    idle: { status: 'degraded', label: 'Waiting' },
    completed: { status: 'idle', label: 'Completed' },
    failed: { status: 'error', label: 'Failed' },
    budget_exhausted: { status: 'error', label: 'Budget exhausted' },
  };

export function runStatusDot(row: RunRow) {
  const state = RUN_STATES[row.state];
  return (
    <Stack direction="horizontal" align="center" gap="snug">
      <StatusDot
        status={state?.status ?? 'idle'}
        glow={state?.status === 'healthy'}
        pulse={state?.status === 'pending'}
      />
      <Text size="supporting">{state?.label ?? row.state}</Text>
    </Stack>
  );
}
