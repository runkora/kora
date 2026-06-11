'use client';

/** @tier 3 — catalog-scoped. Promote to src/components/ if a second feature needs this. */

import { Alert, Badge, Icon, Stack, Text } from '@orbit/terra';
import { fmtDate } from '@orbit/utils';

export interface SyncSummary {
  lastSyncedAt: string | null; // RFC3339 or null if no pricing data
  modelsWithPricing: number;
  pricingFetchFailed: boolean;
  isStale: boolean; // computed server-side: last sync > 25 h ago
}

interface SyncStatusBannerProps {
  sync: SyncSummary;
}

export function SyncStatusBanner({ sync }: SyncStatusBannerProps) {
  if (sync.pricingFetchFailed) {
    return (
      <Alert variant="warning">
        <Stack direction="horizontal" align="center" gap="snug">
          <Icon name="alert-circle" size="2xs" color="status-degraded" />
          <Text size="caption" color="muted">
            Pricing data unavailable — effective prices cannot be displayed.
          </Text>
        </Stack>
      </Alert>
    );
  }

  if (!sync.lastSyncedAt || sync.modelsWithPricing === 0) {
    return (
      <Alert variant="warning">
        <Stack direction="horizontal" align="center" gap="snug">
          <Icon name="alert-circle" size="2xs" color="status-degraded" />
          <Text size="caption" color="muted">
            No pricing data synced yet. Prices will appear after the first scheduled sync.
          </Text>
        </Stack>
      </Alert>
    );
  }

  const { isStale } = sync;

  if (isStale) {
    return (
      <Alert variant="warning">
        <Stack direction="horizontal" align="center" gap="snug" wrap>
          <Icon name="alert-circle" size="2xs" color="status-degraded" />
          <Text size="caption" color="muted">
            Pricing data may be stale — last synced {fmtDate(sync.lastSyncedAt)}
          </Text>
          <Badge variant="degraded" size="sm">
            {sync.modelsWithPricing} model{sync.modelsWithPricing !== 1 ? 's' : ''} with pricing
          </Badge>
        </Stack>
      </Alert>
    );
  }

  return (
    <Stack direction="horizontal" align="center" gap="snug" wrap>
      <Icon name="check-circle" size="2xs" color="status-healthy" />
      <Text size="caption" color="muted">
        Pricing in sync — last synced {fmtDate(sync.lastSyncedAt)}
      </Text>
      <Badge variant="healthy" size="sm">
        {sync.modelsWithPricing} model{sync.modelsWithPricing !== 1 ? 's' : ''} with pricing
      </Badge>
    </Stack>
  );
}
