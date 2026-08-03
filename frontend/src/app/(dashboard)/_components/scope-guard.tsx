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

  // Optimistic while the session loads: enforcement lives at the API layer
  // (the server-rendered content behind this guard was fetched under the
  // user's own token), so hiding it here only blanks the page — including
  // the route's loading.tsx skeleton — until the client session fetch
  // resolves. Render children and swap to Forbidden only on a confirmed
  // missing scope.
  if (!loaded) return <>{children}</>;

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
