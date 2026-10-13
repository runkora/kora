/**
 * @tier 2 — App-level shared. Cross-feature; maps Orbit status tokens to a
 * labelled Badge with status dot. Used by dashboard home, model-gateway, policy,
 * and any future feature overview showing agent or resource health.
 * @promote-to-tier-1 if: domain knowledge (OrbitStatus type, label capitalisation)
 * is removed and a second non-dashboard kora surface needs it.
 */
import { Badge } from '@orbit/hades';

export type OrbitStatus = 'healthy' | 'degraded' | 'error' | 'idle' | 'pending' | 'unknown';

export interface StatusBadgeProps {
  status: OrbitStatus;
  /** Override the auto-capitalised default label. */
  label?: string;
}

const DEFAULT_LABELS: Record<OrbitStatus, string> = {
  healthy: 'Healthy',
  degraded: 'Degraded',
  error: 'Error',
  idle: 'Idle',
  pending: 'Pending',
  unknown: 'Unknown',
};

export function StatusBadge({ status, label }: StatusBadgeProps) {
  const variant = status === 'unknown' ? ('muted' as const) : status;
  return (
    <Badge variant={variant} dot>
      {label ?? DEFAULT_LABELS[status]}
    </Badge>
  );
}
