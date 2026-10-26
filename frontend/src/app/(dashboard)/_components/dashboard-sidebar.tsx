'use client';

import { usePathname } from 'next/navigation';
import Link from 'next/link';

import { Icon, NavItem, Navigation, Stack, Text } from '@runkora/hades';
import { hasScopeCovering, hasScopeWithin } from '@runkora/lib/permissions';
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
  const { scopes } = useSession();

  return (
    <Navigation aria-label="Main">
      <Stack gap="comfortable" paddingInline="cozy" paddingBlock="relaxed">
        {NAV_GROUPS.map(({ label, items }) => {
          // Filter out entries that require a scope the user doesn't have.
          const visibleItems = items.filter(
            (entry) =>
              !entry.requiredScope ||
              (entry.allowNarrowerScope ? hasScopeWithin : hasScopeCovering)(
                scopes,
                entry.requiredScope,
              ),
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
                    <Link href={href} onClick={onNavigate}>
                      {itemLabel}
                    </Link>
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
