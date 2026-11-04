'use client';

/** @tier 3 — skills-scoped. A failed load with its reason and a retry, on the overview and the skill page. */

import { useTransition } from 'react';
import { useRouter } from 'next/navigation';
import { Alert, Button, Icon, Stack, Text } from '@runkora/hades';
import type { SkillsLoadError } from '../_types';

interface LoadErrorAlertProps {
  error: SkillsLoadError;
  /** Defaults to re-rendering the page on the server. */
  onRetry?: () => void;
}

export function LoadErrorAlert({ error, onRetry }: LoadErrorAlertProps) {
  const router = useRouter();
  const [pending, startRetry] = useTransition();

  return (
    <Alert variant="error">
      <Icon name="triangle-alert" color="status-error" />
      <Stack grow>
        {error.status === 502 && (
          <Text size="caption" weight="heavy" color="status-error">
            This Marketplace is unavailable.
          </Text>
        )}
        <Text size="caption" color="status-error">
          {error.message}
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
