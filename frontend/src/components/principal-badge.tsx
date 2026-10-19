/**
 * @tier 2 — kora-domain-aware: maps a principal type to its badge colour. Used by:
 * policy (grants, grant detail, evaluate), model-gateway budgets.
 */
import { Badge } from '@runkora/hades';

export type PrincipalType = 'AGENT' | 'HUMAN' | 'PAT' | 'SERVICE';

export interface PrincipalBadgeProps {
  /** A principal type. Anything that is not agent, human or PAT takes the service colour. */
  type: PrincipalType | (string & {});
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
