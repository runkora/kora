'use client';

import { usePathname } from 'next/navigation';

import { Badge, Icon, NavItem, Navigation, Separator, Stack, Text } from '@orbit/terra';
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
  const { scopes } = useSession();

  return (
    <Stack direction="vertical">
        {/* Logo */}
        <Stack direction="horizontal" align="center" justify="between" height="header" paddingInline="snug">
            <Stack direction="horizontal" align="center" gap="snug">
              <Icon name="zap" size="sm" container containerVariant="accent" />
              <Text size="supporting" weight="strong">
                Orbit
              </Text>
            </Stack>
            <Badge variant="outline" size="sm">
              v0.4.0
            </Badge>
        </Stack>
        <Separator />

        {/* Nav groups */}
        <Navigation aria-label="Main" grow overflowY="auto">
          <Stack gap="spacious" paddingInline="snug" paddingBlock="snug">
          {NAV_GROUPS.map(({ label, items }) => {
            // Filter out entries that require a scope the user doesn't have.
            const visibleItems = items.filter(
              (entry) => !entry.requiredScope || hasScope(scopes, entry.requiredScope),
            );
            if (visibleItems.length === 0) return null;

            return (
              <Stack key={label} gap="none">
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
              </Stack>
            );
          })}
          </Stack>
        </Navigation>
    </Stack>
  );
}
