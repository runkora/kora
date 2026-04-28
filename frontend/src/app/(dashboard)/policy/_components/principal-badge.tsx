/**
 * @tier 3 — policy scoped. Maps Orbit principal type to a badge variant.
 * @promote-to-tier-2 if: a second feature area outside policy needs to display principal types.
 */
import { Badge } from '@orbit/terra';
import type { PrincipalType } from '../_types';

export interface PrincipalBadgeProps {
  type: PrincipalType;
}

export function PrincipalBadge({ type }: PrincipalBadgeProps) {
  const variant =
    type === 'AGENT'
      ? 'default'
      : type === 'HUMAN'
        ? 'healthy'
        : type === 'PAT'
          ? 'secondary'
          : 'degraded';
  return (
    <Badge variant={variant} size="sm">
      {type}
    </Badge>
  );
}
