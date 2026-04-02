'use client';

import { useEffect, useState } from 'react';
import { User, Zap } from 'lucide-react';
import { usePathname } from 'next/navigation';

import { Badge, NavItem, Separator, Text } from '@orbit/terra';
import { LogoutButton } from '@orbit/components/logout-button';
import { ApiRoute } from '@orbit/lib/routing/api-routes';
import { NAV_GROUPS } from '../_utils';

// ─── DashboardSidebar ─────────────────────────────────────────────────────────

/**
 * Primary navigation sidebar for the dashboard shell.
 *
 * Active state is derived from the current pathname so it stays correct across
 * client-side navigations without any prop drilling.
 */
export function DashboardSidebar() {
  const pathname = usePathname();
  const [userEmail, setUserEmail] = useState('');
  const [userName, setUserName] = useState('');

  useEffect(() => {
    fetch(ApiRoute.session.GET())
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => {
        if (data?.user) {
          setUserEmail(data.user.email || '');
          setUserName(data.user.name || '');
        }
      })
      .catch(() => {});
  }, []);

  return (
    <div className="flex h-full flex-col">
      {/* Logo */}
      <div className="flex h-14 shrink-0 items-center gap-2 px-4 border-b border-border-subtle">
        <div className="flex size-7 items-center justify-center rounded-md bg-accent/15 text-accent">
          <Zap className="size-4" aria-hidden />
        </div>
        {/*
         * Raw span: carries both layout (flex alignment) and brand typography
         * that doesn't map to a Terra Heading variant — this is a logotype.
         */}
        <span className="text-sm font-semibold tracking-tight text-foreground">Orbit</span>
        {/*
         * ml-auto wrapper: pushes the version badge to the trailing edge of the
         * logo row. Layout concern on the wrapper, not the Badge.
         */}
        <div className="ml-auto">
          <Badge variant="outline" size="sm">
            v0.4.0
          </Badge>
        </div>
      </div>

      {/* Nav groups */}
      <nav className="flex-1 overflow-y-auto px-3 py-4 space-y-5">
        {NAV_GROUPS.map(({ label, items }) => (
          <div key={label} className="space-y-0.5">
            {/*
             * px-3 pb-1.5 wrapper: structural spacing for the nav group label
             * row. Layout concerns belong on the wrapper, not on Text itself.
             */}
            <div className="px-3 pb-1.5">
              <Text size="caption" color="subtle" weight="emphasis" transform="uppercase">
                {label}
              </Text>
            </div>
            {items.map(({ label: itemLabel, href, icon: Icon, badge, wip }) =>
              wip ? (
                <NavItem
                  key={itemLabel}
                  state="disabled"
                  icon={<Icon />}
                  badge={badge}
                >
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
              )
            )}
          </div>
        ))}
      </nav>

      {/* Footer */}
      <Separator />
      <div className="flex items-center gap-2.5 px-4 py-3">
        <div className="flex size-7 items-center justify-center rounded-full bg-surface-elevated text-muted">
          <User className="size-3.5" aria-hidden />
        </div>
        <div className="flex-1 min-w-0">
          {/*
           * truncate: intrinsic to this content — email addresses can be
           * arbitrarily long and must not overflow the sidebar column.
           */}
          <Text size="caption" weight="emphasis" truncate>
            {userEmail || 'Loading…'}
          </Text>
          {userName && (
            <Text size="caption" color="subtle" truncate>
              {userName}
            </Text>
          )}
        </div>
        <LogoutButton />
      </div>
    </div>
  );
}
