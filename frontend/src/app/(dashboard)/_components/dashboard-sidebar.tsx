'use client';

import { usePathname, useRouter } from 'next/navigation';
import Link from 'next/link';

import { Icon, NavItem, Navigation, Stack, Text } from '@runkora/hades';
import { hasScopeCovering, hasScopeWithin } from '@runkora/lib/permissions';
import { APP_ROUTES } from '@runkora/lib/routing';
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
  const router = useRouter();
  // The Skills entry stays active on every skill page.
  const activePathname = pathname.startsWith(`${APP_ROUTES.skills.root}/`)
    ? APP_ROUTES.skills.root
    : pathname;
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
                    state={activePathname === href ? 'active' : 'default'}
                    aria-current={activePathname === href ? 'page' : undefined}
                    icon={<Icon name={icon} />}
                    badge={badge}
                    asChild
                  >
                    {/* Prefetched when a mouse or focus reaches the link, not when it
                        renders: every page load would otherwise fetch every route twice.
                        Touch is left out on purpose: iOS fires synthetic mouse and touch
                        events before the click, and work in them can make Safari drop the
                        tap, so links needed a long press. */}
                    <Link
                      href={href}
                      prefetch={false}
                      onClick={onNavigate}
                      onPointerEnter={(e) => {
                        if (e.pointerType === 'mouse') router.prefetch(href);
                      }}
                      onFocus={() => router.prefetch(href)}
                    >
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
