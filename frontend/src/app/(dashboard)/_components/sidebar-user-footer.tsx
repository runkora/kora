'use client';

import { Icon, Separator, Stack, Text } from '@orbit/terra';
import { LogoutButton } from '@orbit/components';
import { useSession } from './permissions-provider';

/**
 * User identity + logout row rendered inside PageLayout's sidebarFooter slot.
 * PageLayout wraps this in a semantic <footer> element internally.
 */
export function SidebarUserFooter() {
  const { user } = useSession();

  return (
    <>
      <Separator />
      <Stack
        direction="horizontal"
        align="center"
        gap="snug"
        paddingBlock="snug"
        paddingInline="comfortable"
      >
        <Icon name="user" size="xs" container containerVariant="surface" />
        <Stack direction="vertical" grow minWidth="none">
          <Text size="caption" weight="emphasis" truncate>
            {user?.email || 'Loading…'}
          </Text>
          {user?.name && (
            <Text size="caption" color="subtle" truncate>
              {user.name}
            </Text>
          )}
        </Stack>
        <LogoutButton />
      </Stack>
    </>
  );
}
