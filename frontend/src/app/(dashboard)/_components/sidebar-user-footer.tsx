'use client';

import { useCallback, useState } from 'react';
import Link from 'next/link';
import {
  Button,
  Icon,
  NavItem,
  Popover,
  PopoverContent,
  PopoverTrigger,
  Separator,
  Stack,
  Text,
} from '@orbit/terra';
import { APP_ROUTES } from '@orbit/lib/routing/routes';
import { useLogout } from '@orbit/lib/use-logout';
import { useSession } from './permissions-provider';

const TRIGGER_CLASS =
  'm-0 w-full rounded-md cursor-pointer select-none transition-colors duration-150 ' +
  'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent ' +
  'text-muted hover:bg-surface-elevated hover:text-foreground';

/**
 * User identity + popover menu rendered inside PageLayout's sidebarFooter slot.
 * PageLayout wraps this in a semantic <footer> element internally.
 *
 * The popover replaces the inline LogoutButton (ADR-0061) and provides
 * navigation to the profile page plus a placeholder for future settings.
 */
export function SidebarUserFooter() {
  const { user } = useSession();
  const { isLoggingOut, logout } = useLogout();
  const [open, setOpen] = useState(false);
  const close = useCallback(() => setOpen(false), []);

  return (
    <>
      <Separator />
      <Stack paddingInline="cozy" paddingBlock="snug">
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <Stack
              asChild
              direction="horizontal"
              align="center"
              gap="snug"
              paddingBlock="snug"
              paddingInline="cozy"
            >
              <figure role="button" tabIndex={0} aria-label="User menu" className={TRIGGER_CLASS}>
                <Icon name="user" size="sm" container containerVariant="muted" circle />
                <Stack direction="vertical" grow minWidth="none" gap="none">
                  <Text size="caption" weight="emphasis" truncate>
                    {user?.email || 'Loading\u2026'}
                  </Text>
                  {user?.name && (
                    <Text size="caption" color="subtle" truncate>
                      {user.name}
                    </Text>
                  )}
                </Stack>
                <Icon name="chevron-up" size="2xs" color="subtle" />
              </figure>
            </Stack>
          </PopoverTrigger>
          <PopoverContent
            side="top"
            align="start"
            style={{ width: 'var(--radix-popover-trigger-width)' }}
          >
            <Stack gap="none">
              <NavItem size="sm" icon={<Icon name="user" />} asChild>
                <Link href={APP_ROUTES.profile} onClick={close}>
                  View Profile
                </Link>
              </NavItem>
              <NavItem
                size="sm"
                state="disabled"
                icon={<Icon name="settings" />}
                style={{ textAlign: 'left' }}
              >
                Settings
              </NavItem>
              <Separator span="full" />
              <NavItem
                size="sm"
                state={isLoggingOut ? 'disabled' : 'default'}
                icon={<Icon name={isLoggingOut ? 'loader' : 'log-out'} color="status-error" />}
                asChild
              >
                <Button
                  variant="ghost"
                  onClick={() => void logout()}
                  disabled={isLoggingOut}
                  style={{ textAlign: 'left' }}
                >
                  <Text as="span" size="caption" color="status-error">
                    {isLoggingOut ? 'Logging out\u2026' : 'Log out'}
                  </Text>
                </Button>
              </NavItem>
            </Stack>
          </PopoverContent>
        </Popover>
      </Stack>
    </>
  );
}
