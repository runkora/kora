/**
 * @tier 2 — kora-domain-aware: run states and their status colours. A run's state as one icon.
 * Used by: the agents pages' run rows, the Runs page.
 */

import { Icon, type IconColor, type IconName } from '@runkora/hades';
import { RUN_STATE_DISPLAY, type RunStateDisplay } from './run-state';

const STATE_ICON: Partial<Record<RunStateDisplay['status'], { name: IconName; color: IconColor }>> =
  {
    healthy: { name: 'loader', color: 'status-healthy' },
    pending: { name: 'clock', color: 'status-pending' },
    degraded: { name: 'pause', color: 'status-degraded' },
    error: { name: 'circle-x', color: 'status-error' },
    idle: { name: 'circle-check', color: 'muted' },
  };

export interface RunStateIconProps {
  state: string;
}

/** The run's state as an icon, named for screen readers with the state's label. */
export function RunStateIcon({ state }: RunStateIconProps) {
  const display = RUN_STATE_DISPLAY[state] ?? { status: 'idle', label: state };
  const icon = STATE_ICON[display.status] ?? { name: 'circle-check', color: 'muted' };
  return (
    <Icon
      name={icon.name}
      size="sm"
      color={icon.color}
      label={display.label}
      shrink
      spin={icon.name === 'loader'}
    />
  );
}
