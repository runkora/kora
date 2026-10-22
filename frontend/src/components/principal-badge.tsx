/**
 * @tier 2 — kora-domain-aware: maps a principal type to its badge colour and label. The
 * `SERVICE` wire value reads "Trigger" (triggers are the service identities users see); every
 * other type shows as it is. Used by: policy (grants, grant detail, evaluate), model-gateway
 * budgets.
 */
import { Badge } from '@runkora/hades';

export type PrincipalType = 'AGENT' | 'HUMAN' | 'PAT' | 'SERVICE';

const PRINCIPAL_LABELS: Record<string, string> = { SERVICE: 'Trigger' };

/** What a principal type is called in the UI. The wire value is unchanged. */
export function principalLabel(type: PrincipalType | (string & {})): string {
  return Object.hasOwn(PRINCIPAL_LABELS, type) ? PRINCIPAL_LABELS[type]! : type;
}

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
      {principalLabel(type)}
    </Badge>
  );
}
