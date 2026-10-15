'use client';

/** @tier 3 — marketplace-scoped. The failed-load notice of an open Skill, with a retry. */

import { Alert, Button, Icon, Stack, Text } from '@orbit/hades';

interface MarketplaceSkillLoadErrorProps {
  /** Sends the Skill request again. The view shows the skeleton until it completes. */
  onRetry: () => void;
}

/** Shown when the open Skill could not load. This is not a missing Skill. */
export function MarketplaceSkillLoadError({ onRetry }: MarketplaceSkillLoadErrorProps) {
  return (
    <Alert variant="error">
      <Icon name="triangle-alert" color="status-error" />
      <Stack grow>
        <Text size="caption" color="status-error">
          Orbit could not load this Skill from the Marketplace.
        </Text>
      </Stack>
      <Button variant="outline" size="xs" onClick={onRetry}>
        Try again
      </Button>
    </Alert>
  );
}
