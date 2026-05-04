'use client';

import type { ReactNode } from 'react';

import { Stack, Text } from '@orbit/terra';
import { hasScope } from '@orbit/lib/permissions';
import { useSession } from './permissions-provider';

/**
 * Page-level scope guard. Renders children only when the current principal
 * holds the required scope. Shows a forbidden message otherwise.
 *
 * Designed for use in page layouts where the sidebar hides the nav entry
 * but a direct URL navigation should still be handled gracefully.
 */
export function ScopeGuard({ scope, children }: { scope: string; children: ReactNode }) {
  const { scopes, loaded } = useSession();

  if (!loaded) return null;

  if (!hasScope(scopes, scope)) {
    return (
      <Stack direction="vertical" align="center" justify="center" minHeight="full">
        <Text size="lead" weight="emphasis">
          Forbidden
        </Text>
        <Text color="subtle">You do not have permission to view this page.</Text>
      </Stack>
    );
  }

  return <>{children}</>;
}
