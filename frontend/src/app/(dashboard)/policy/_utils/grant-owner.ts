import { APP_ROUTES } from '@runkora/lib/routing/routes';
import type { Grant } from '../_types';

export interface GrantOwnerLink {
  href: string;
  label: string;
}

// Trigger grants are keyed `trigger:<id>`, or `trigger-source:<id>` for a webhook source.
const TRIGGER_ID = /^trigger(?:-source)?:\S.*/;

// An agent's principal ID is its SPIFFE ID, spiffe://<trust-domain>/tenant/<ns>/agent/<slug>.
// Anything else (a hand-made grant keyed by a UUID) has no agent to link to.
function agentSlug(principalId: string): string | null {
  if (typeof principalId !== 'string' || !principalId.startsWith('spiffe://')) return null;
  const segments = principalId.slice('spiffe://'.length).split('/');
  // trust domain, then at least `agent` and the slug; a trailing slash leaves an empty slug.
  if (segments.length < 3 || segments[segments.length - 2] !== 'agent') return null;
  const slug = segments[segments.length - 1]!;
  return slug === '' || slug === '.' || slug === '..' ? null : slug;
}

/**
 * The screen that owns a grant the grants page cannot change: the agent builder for an agent, the
 * triggers list for a trigger, the token screen for a PAT. Null for a person, whose grants are
 * managed here, and for an agent or trigger id that cannot be read (a hand-made grant keyed by a
 * UUID), which stays read-only with nowhere to link.
 */
export function grantOwnerLink(
  grant: Pick<Grant, 'principal_type' | 'principal_id'>,
): GrantOwnerLink | null {
  switch (grant.principal_type) {
    case 'AGENT': {
      const slug = agentSlug(grant.principal_id);
      return slug
        ? { href: APP_ROUTES.agents.detail(encodeURIComponent(slug)), label: 'Open agent' }
        : null;
    }
    case 'SERVICE':
      return TRIGGER_ID.test(grant.principal_id)
        ? { href: APP_ROUTES.triggers, label: 'Open triggers' }
        : null;
    case 'PAT':
      return { href: APP_ROUTES.modelGateway.keys, label: 'Open API keys' };
    default:
      return null;
  }
}

/** Whether the grants page may delete or re-budget a grant: only a person's. */
export function isGrantEditable(grant: Pick<Grant, 'principal_type'>): boolean {
  return grant.principal_type === 'HUMAN';
}
