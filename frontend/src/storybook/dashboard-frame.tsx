'use client';

/**
 * The dashboard chrome for page stories: sidebar, header and the providers the
 * real layout wraps every page in. It mirrors `app/(dashboard)/layout.tsx`
 * without the server-side session and preferences lookups.
 */

import * as React from 'react';
import type { Decorator } from '@storybook/react';
import { PageLayout, TooltipProvider } from '@orbit/hades';
import {
  DashboardHeader,
  DashboardSidebar,
  MobileSidebarDrawer,
  PageGutter,
  PermissionsProvider,
  SidebarBrandHeader,
  SidebarUserFooter,
} from '../app/(dashboard)/_components';
import { SidebarStateProvider } from '../app/(dashboard)/_components/sidebar-state';
import { PreferencesProvider } from '../app/(dashboard)/_preferences/preferences-context';
import { PREFERENCE_DEFAULTS } from '../app/(dashboard)/_preferences/preferences.schema';
import { mockApi, type MockRoutes } from './mock-api';

/** Enough scopes to show every navigation entry. */
export const ALL_NAV_SCOPES = [
  'triggers:read',
  'platform:grants:read',
  'platform:roles:read',
  'platform:routes:read',
  'platform:vocabulary:read',
];

export const MOCK_USER = { sub: 'usr_42b7e9', email: 'dev@kora.example', name: 'Dev Operator' };

/** The session answer the permissions provider asks for on mount. */
export function sessionRoutes(scopes: string[] = ALL_NAV_SCOPES): MockRoutes {
  return {
    'GET /api/auth/session': {
      user: MOCK_USER,
      permissions: { scopes },
      expiresAt: Math.floor(Date.now() / 1000) + 3600,
    },
  };
}

/** A route layout, such as `policy/layout.tsx`, that adds a scope guard. */
export type RouteLayout = React.ComponentType<{ children: React.ReactNode }>;

export function DashboardFrame({
  children,
  routeLayout: RouteLayout,
}: {
  children: React.ReactNode;
  routeLayout?: RouteLayout;
}) {
  return (
    <TooltipProvider>
      <PermissionsProvider>
        <SidebarStateProvider>
          <PreferencesProvider initialPreferences={PREFERENCE_DEFAULTS}>
            <PageLayout
              sidebarHeader={<SidebarBrandHeader />}
              sidebar={<DashboardSidebar />}
              sidebarFooter={<SidebarUserFooter />}
              header={<DashboardHeader />}
              mobileDrawer={<MobileSidebarDrawer />}
            >
              <PageGutter>
                {RouteLayout ? <RouteLayout>{children}</RouteLayout> : children}
              </PageGutter>
            </PageLayout>
          </PreferencesProvider>
        </SidebarStateProvider>
      </PermissionsProvider>
    </TooltipProvider>
  );
}

/**
 * Story decorator: wraps the story in the dashboard chrome and answers the
 * session call plus whatever page routes the story passes in. Pass a route
 * layout only when it does more than pad, such as a scope guard.
 */
export function dashboardPage(routes: MockRoutes = {}, routeLayout?: RouteLayout): Decorator {
  const withApi = mockApi({ ...sessionRoutes(), ...routes });
  const WithDashboard: Decorator = (Story, context) =>
    withApi(
      () => (
        <DashboardFrame {...(routeLayout ? { routeLayout } : {})}>
          <Story />
        </DashboardFrame>
      ),
      context,
    );
  return WithDashboard;
}
