/** @tier 3 — budgets-page-scoped. Principal display-name resolution. */

import { agentNameFromPrincipalId } from '@orbit/utils';
import type { PrincipalIdentity } from '../../_types';

export interface PrincipalDisplay {
  /** Human-friendly primary label: user name/email, agent slug, or the raw ID. */
  primary: string;
  /** Secondary line (email or raw ID); null when it would duplicate the primary. */
  secondary: string | null;
}

/**
 * Resolves how a principal is shown: humans via identity attribution
 * (display name, then email), agents via their SPIFFE slug, everything else
 * falls back to the raw ID — always keeping the raw ID reachable on the
 * secondary line.
 */
export function principalDisplay(
  principalId: string,
  principalType: string | undefined,
  identity: PrincipalIdentity | null | undefined,
): PrincipalDisplay {
  if (principalType === 'AGENT') {
    const name = agentNameFromPrincipalId(principalId);
    if (name) {
      return { primary: name, secondary: principalId };
    }
  }
  if (identity?.display_name || identity?.email) {
    return {
      primary: identity.display_name || identity.email || principalId,
      secondary: identity.email ?? principalId,
    };
  }
  return { primary: principalId, secondary: null };
}
