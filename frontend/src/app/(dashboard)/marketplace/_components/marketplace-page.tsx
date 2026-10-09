'use client';

/** @tier 3 — marketplace-scoped. Read-only list of available skill marketplaces. */

import Link from 'next/link';
import { Heading, Stack, Text } from '@orbit/terra';
import { EmptyState } from '@orbit/components';
import { APP_ROUTES } from '@orbit/lib/routing';
import type { Marketplace } from '@orbit/lib/marketplace';
import { marketplaceSlug } from '@orbit/lib/marketplace';

interface MarketplacePageProps {
  marketplaces: Marketplace[];
  error: string | null;
}

export function MarketplacePage({ marketplaces, error }: MarketplacePageProps) {
  return (
    <Stack gap="comfortable">
      <Stack gap="tight">
        <Heading variant="title">Marketplace</Heading>
        <Text size="supporting" color="muted">
          Browse skill marketplaces and install skills into your agents.
        </Text>
      </Stack>

      {error ? (
        <Text size="supporting" color="muted">
          {error}
        </Text>
      ) : marketplaces.length === 0 ? (
        <EmptyState
          icon="box"
          title="No marketplaces"
          description="No skill marketplaces are configured for this tenant yet."
        />
      ) : (
        <Stack gap="snug">
          {marketplaces.map((m) => (
            <Link
              key={m.name}
              href={APP_ROUTES.marketplace.skills(marketplaceSlug(m.name))}
              style={{ textDecoration: 'none' }}
            >
              <Stack
                direction="horizontal"
                align="center"
                justify="between"
                padding="comfortable"
                gap="comfortable"
              >
                <Stack gap="tight">
                  <Text weight="emphasis">{m.name}</Text>
                  <Text size="supporting" color="muted">
                    Browse skills →
                  </Text>
                </Stack>
              </Stack>
            </Link>
          ))}
        </Stack>
      )}
    </Stack>
  );
}
