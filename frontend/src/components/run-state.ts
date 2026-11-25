/**
 * @tier 2 — kora-domain-aware: maps run states to a status dot and a label.
 * Used by: RunStateIcon.
 */
import type { StatusDotProps } from '@runkora/hades';

export interface RunStateDisplay {
  status: NonNullable<StatusDotProps['status']>;
  label: string;
}

/** Each run state's dot and label. */
export const RUN_STATE_DISPLAY: Record<string, RunStateDisplay> = {
  pending: { status: 'pending', label: 'Queued' },
  starting: { status: 'pending', label: 'Starting' },
  active: { status: 'healthy', label: 'Running' },
  completing: { status: 'healthy', label: 'Finishing' },
  idle: { status: 'degraded', label: 'Waiting' },
  completed: { status: 'idle', label: 'Completed' },
  failed: { status: 'error', label: 'Failed' },
  budget_exhausted: { status: 'error', label: 'Budget exhausted' },
  terminated: { status: 'idle', label: 'Terminated' },
};
