'use client';

/** @tier 3 — marketplace/[marketplaceSlug]/[skillName]-scoped. Retries the failed server render. */

import { useTransition } from 'react';
import { useRouter } from 'next/navigation';
import { Alert, Button, Icon, Stack, Text } from '@orbit/hades';

/** Shown when the open Skill could not load. This is not a missing Skill. */
export function MarketplaceSkillLoadError() {
  const router = useRouter();
  const [pending, startRetry] = useTransition();

  return (
    <Alert variant="error">
      <Icon name="triangle-alert" color="status-error" />
      <Stack grow>
        <Text size="caption" color="status-error">
          Orbit could not load this Skill from the Marketplace.
        </Text>
      </Stack>
      <Button
        variant="outline"
        size="xs"
        disabled={pending}
        onClick={() => startRetry(() => router.refresh())}
      >
        Try again
      </Button>
    </Alert>
  );
}
