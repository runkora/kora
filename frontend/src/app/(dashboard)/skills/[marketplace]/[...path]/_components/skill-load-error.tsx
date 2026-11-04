'use client';

/** @tier 3 — skills/[marketplace]/[...path]-scoped. A skill that could not load, with a retry of the server render. */

import { useTransition } from 'react';
import { useRouter } from 'next/navigation';
import { Alert, Button, Icon, Stack, Text } from '@runkora/hades';

export function SkillLoadError({ status, message }: { status: number; message: string }) {
  const router = useRouter();
  const [pending, startRetry] = useTransition();

  return (
    <Alert variant="error">
      <Icon name="triangle-alert" color="status-error" />
      <Stack grow>
        {status === 502 && (
          <Text size="caption" weight="heavy" color="status-error">
            This Marketplace is unavailable.
          </Text>
        )}
        <Text size="caption" color="status-error">
          {message}
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
