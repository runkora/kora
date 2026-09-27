'use client';

/** @tier 3 — marketplace-scoped. Client boundary: the failed-load notice of the Skill page, with a retry. */

import { useTransition } from 'react';
import { useRouter } from 'next/navigation';
import { Alert, Button, Icon, Stack, Text } from '@orbit/terra';

/**
 * Shown when the Skill page could not load its Skill. "Try again" renders the
 * page on the server again (router.refresh), so the page loads the Skill
 * again. The button stays disabled until that render completes.
 */
export function MarketplaceSkillLoadError() {
  const router = useRouter();
  const [retrying, startRetry] = useTransition();

  function handleRetry() {
    startRetry(() => {
      router.refresh();
    });
  }

  return (
    <Alert variant="error">
      <Icon name="triangle-alert" color="status-error" />
      <Stack grow>
        <Text size="caption" color="status-error">
          Orbit could not load this Skill from the Marketplace.
        </Text>
      </Stack>
      <Button variant="outline" size="xs" onClick={handleRetry} disabled={retrying}>
        Try again
      </Button>
    </Alert>
  );
}
