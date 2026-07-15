'use client';

import { usePathname } from 'next/navigation';

import { Icon, NavItem, Navigation, Stack, Text } from '@orbit/terra';
import { hasScope } from '@orbit/lib/permissions';
import { AppLink } from '@orbit/components/links';
import { NAV_GROUPS } from '../_utils';
import { useSession } from './permissions-provider';

// ─── DashboardSidebar ─────────────────────────────────────────────────────────

/**
 * Primary navigation sidebar for the dashboard shell.
 *
 * Active state is derived from the current pathname so it stays correct across
 * client-side navigations without any prop drilling.
 *
 * `onNavigate` is called when any nav link is clicked — used by the mobile
 * drawer to close itself after navigation.
 */
export function DashboardSidebar({ onNavigate }: { onNavigate?: () => void }) {
  const pathname = usePathname();
  const { scopes, devEnvironmentId } = useSession();

  return (
    <Navigation aria-label="Main">
      <Stack gap="comfortable" paddingInline="cozy" paddingBlock="relaxed">
        {NAV_GROUPS.map(({ label, items }) => {
          // Filter out entries that require a scope the user doesn't have, or
          // that shouldn't appear when this install is itself a dev environment.
          const visibleItems = items.filter(
            (entry) =>
              (!entry.requiredScope || hasScope(scopes, entry.requiredScope)) &&
              !(entry.hideInDevEnv && devEnvironmentId),
          );
          if (visibleItems.length === 0) return null;

          return (
            <Stack key={label} gap="none">
              <Stack paddingTop="none" paddingInline="cozy" paddingBottom="tight">
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
                    <AppLink to={href} onClick={onNavigate}>
                      {itemLabel}
                    </AppLink>
                  </NavItem>
                ),
              )}
            </Stack>
          );
        })}
      </Stack>
    </Navigation>
  );
}
