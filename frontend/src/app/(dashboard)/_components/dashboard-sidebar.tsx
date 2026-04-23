'use client';

import { usePathname } from 'next/navigation';

import { Badge, Icon, NavItem, Separator, Stack, Text } from '@orbit/terra';
import { LogoutButton } from '@orbit/components/logout-button';
import { hasScope } from '@orbit/lib/permissions';
import { NAV_GROUPS } from '../_utils';
import { useSession } from './permissions-provider';

// ─── DashboardSidebar ─────────────────────────────────────────────────────────

/**
 * Primary navigation sidebar for the dashboard shell.
 *
 * Active state is derived from the current pathname so it stays correct across
 * client-side navigations without any prop drilling.
 */
export function DashboardSidebar() {
  const pathname = usePathname();
  const { scopes, user } = useSession();

  return (
    <Stack direction="vertical" asChild>
      <aside className="h-full">
        {/* Logo */}
        <Stack direction="horizontal" align="center" justify="between" asChild>
          <header className="h-14 shrink-0 px-4 border-b border-border-subtle">
            <Stack direction="horizontal" align="center" gap="snug">
              <Icon name="zap" size="sm" container containerVariant="accent" />
              <Text size="supporting" weight="strong">
                Orbit
              </Text>
            </Stack>
            <Badge variant="outline" size="sm">
              v0.4.0
            </Badge>
          </header>
        </Stack>

        {/* Nav groups */}
        <nav className="flex-1 overflow-y-auto px-3 py-4 space-y-5">
          {NAV_GROUPS.map(({ label, items }) => {
            // Filter out entries that require a scope the user doesn't have.
            const visibleItems = items.filter(
              (entry) => !entry.requiredScope || hasScope(scopes, entry.requiredScope),
            );
            if (visibleItems.length === 0) return null;

            return (
              <section key={label} className="space-y-0.5">
                <Stack paddingTop="none" paddingInline="snug" paddingBottom="tight">
                  <Text size="caption" color="subtle" weight="emphasis" transform="uppercase">
                    {label}
                  </Text>
                </Stack>
                {visibleItems.map(({ label: itemLabel, href, icon, badge, wip }) =>
                  wip ? (
                    <NavItem
                      key={itemLabel}
                      state="disabled"
                      icon={<Icon name={icon} />}
                      badge={badge}
                    >
                      {itemLabel}
                    </NavItem>
                  ) : (
                    <NavItem
                      key={itemLabel}
                      state={pathname === href ? 'active' : 'default'}
                      icon={<Icon name={icon} />}
                      badge={badge}
                      asChild
                    >
                      <a href={href}>{itemLabel}</a>
                    </NavItem>
                  ),
                )}
              </section>
            );
          })}
        </nav>

        {/* Footer */}
        <Separator />
        <Stack direction="horizontal" align="center" gap="snug" asChild>
          <footer className="px-4 py-3">
            <Icon name="user" size="xs" container containerVariant="surface" />
            {/*
             * grow + minWidth:0 — the standard flex truncation pattern:
             * grow makes the column fill remaining width; minWidth:0 allows it
             * to shrink below its content width so Text truncate can clip long
             * email addresses.
             */}
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
          </footer>
        </Stack>
      </aside>
    </Stack>
  );
}
