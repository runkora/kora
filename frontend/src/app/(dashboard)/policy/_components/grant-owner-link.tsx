'use client';

/**
 * @tier 3 — policy-scoped. Stands in for the delete action on a grant the grants page cannot
 * change: names the screen that owns it. Used by the grant cards, the table and the detail modal.
 * @promote-to-tier-2 if: another feature needs to point at a grant's owner.
 */

import NextLink from 'next/link';
import { Anchor, Text } from '@runkora/hades';
import type { Grant } from '../_types';
import { grantOwnerLink } from '../_utils/grant-owner';

export function GrantOwnerLink({
  grant,
}: {
  grant: Pick<Grant, 'principal_type' | 'principal_id'>;
}) {
  const link = grantOwnerLink(grant);
  if (!link) {
    return (
      <Text size="caption" color="subtle">
        Read-only
      </Text>
    );
  }
  return (
    <Anchor asChild size="sm">
      {/* A row click opens the detail modal; following the link must not. */}
      <NextLink href={link.href} onClick={(e) => e.stopPropagation()}>
        {link.label}
      </NextLink>
    </Anchor>
  );
}
