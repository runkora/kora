import type {
  PATAttributionEntry,
  PATGrantAttribution,
} from '../../model-gateway/_services/tokens-server';
import { agentNameFromPrincipalId } from '@runkora/utils';
import type { Grant } from '../_types';

export interface GrantIdentity {
  primary: string; // display name for HUMAN, agent name for AGENT, else the principal ID
  email?: string; // email for HUMAN
  ownerName?: string; // PAT owner display name
  ownerEmail?: string; // PAT owner email
  rawId?: string; // principal_id, shown when primary is a resolved name
}

export function resolveGrantIdentity(
  grant: Grant,
  patAttribution: Record<string, PATGrantAttribution>,
  userAttribution: Record<string, PATAttributionEntry>,
): GrantIdentity {
  if (grant.principal_type === 'HUMAN') {
    const u = userAttribution[grant.principal_id];
    if (u) {
      return {
        primary: u.display_name || u.email,
        email: u.display_name ? u.email : undefined,
        rawId: grant.principal_id,
      };
    }
    return { primary: grant.principal_id };
  }
  if (grant.principal_type === 'PAT') {
    const pat = patAttribution[grant.principal_id];
    const owner = pat?.owner;
    return {
      primary: pat?.pat_name ?? grant.principal_id,
      ownerName: owner ? owner.display_name || owner.email : undefined,
      ownerEmail: owner?.display_name ? owner.email : undefined,
      rawId: grant.principal_id,
    };
  }
  if (grant.principal_type === 'AGENT') {
    const name = agentNameFromPrincipalId(grant.principal_id);
    if (name) return { primary: name, rawId: grant.principal_id };
  }
  return { primary: grant.principal_id };
}
