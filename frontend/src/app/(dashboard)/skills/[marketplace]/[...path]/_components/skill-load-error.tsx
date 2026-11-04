'use client';

/** @tier 3 — skills/[marketplace]/[...path]-scoped. A skill or file that could not load, with a retry. */

import { useTransition } from 'react';
import { useRouter } from 'next/navigation';
import { Alert, Button, Icon, Stack, Text } from '@runkora/hades';

interface SkillLoadErrorProps {
  status: number;
  message: string;
  /** Defaults to re-rendering the page on the server. */
  onRetry?: () => void;
}

export function SkillLoadError({ status, message, onRetry }: SkillLoadErrorProps) {
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
        onClick={onRetry ?? (() => startRetry(() => router.refresh()))}
      >
        Try again
      </Button>
    </Alert>
  );
}
