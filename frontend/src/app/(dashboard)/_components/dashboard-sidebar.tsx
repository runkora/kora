'use client';

import { User, Zap } from 'lucide-react';
import { usePathname } from 'next/navigation';

import { Badge, NavItem, Separator, Stack, Text } from '@orbit/terra';
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
        <Stack direction="horizontal" align="center" gap="snug" asChild>
          <header className="h-14 shrink-0 px-4 border-b border-border-subtle">
            <span className="flex size-7 items-center justify-center rounded-md bg-accent/15 text-accent">
              <Zap className="size-4" aria-hidden />
            </span>
            {/*
             * Raw span: carries both layout (flex alignment) and brand typography
             * that doesn't map to a Terra Heading variant — this is a logotype.
             */}
            <span className="text-sm font-semibold tracking-tight text-foreground">Orbit</span>
            {/*
             * ml-auto wrapper: pushes the version badge to the trailing edge of the
             * logo row. Layout concern on the wrapper, not the Badge.
             */}
            <span className="ml-auto">
              <Badge variant="outline" size="sm">
                v0.4.0
              </Badge>
            </span>
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
                {/*
                 * px-3 pb-1.5 wrapper: structural spacing for the nav group label
                 * row. Layout concerns belong on the wrapper, not on Text itself.
                 */}
                <span className="block px-3 pb-1.5">
                  <Text size="caption" color="subtle" weight="emphasis" transform="uppercase">
                    {label}
                  </Text>
                </span>
                {visibleItems.map(({ label: itemLabel, href, icon: Icon, badge, wip }) =>
                  wip ? (
                    <NavItem key={itemLabel} state="disabled" icon={<Icon />} badge={badge}>
                      {itemLabel}
                    </NavItem>
                  ) : (
                    <NavItem
                      key={itemLabel}
                      state={pathname === href ? 'active' : 'default'}
                      icon={<Icon />}
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
            <span className="flex size-7 items-center justify-center rounded-full bg-surface-elevated text-muted">
              <User className="size-3.5" aria-hidden />
            </span>
            <Stack direction="vertical" gap="none" asChild>
              <span className="flex-1 min-w-0">
                {/*
                 * truncate: intrinsic to this content — email addresses can be
                 * arbitrarily long and must not overflow the sidebar column.
                 */}
                <Text size="caption" weight="emphasis" truncate>
                  {user?.email || 'Loading…'}
                </Text>
                {user?.name && (
                  <Text size="caption" color="subtle" truncate>
                    {user.name}
                  </Text>
                )}
              </span>
            </Stack>
            <LogoutButton />
          </footer>
        </Stack>
      </aside>
    </Stack>
  );
}
