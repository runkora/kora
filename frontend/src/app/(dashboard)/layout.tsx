'use client';

import { PageLayout } from '@orbit/terra';
import {
  DashboardHeader,
  DashboardSidebar,
  MobileSidebarDrawer,
  PermissionsProvider,
  SidebarBrandHeader,
  SidebarUserFooter,
} from './_components';
import { SidebarStateProvider } from './_components/sidebar-state';
import { PreferencesProvider } from './_preferences/preferences-context';
import { mergeWithDefaults } from './_preferences/preferences.schema';
import { getSession, isSessionValid } from '@orbit/server/session';
import { config } from '@orbit/server/config';

async function fetchPreferences(accessToken: string) {
  try {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 500);
    const res = await fetch(`${config.orbitApiUrl}/identity/preferences`, {
      headers: { Authorization: `Bearer ${accessToken}` },
      cache: 'no-store',
      signal: controller.signal,
    });
    clearTimeout(timeout);
    if (!res.ok) return mergeWithDefaults({});
    const raw: unknown = await res.json();
    return mergeWithDefaults(
      typeof raw === 'object' && raw !== null ? (raw as Record<string, unknown>) : {},
    );
  } catch {
    return mergeWithDefaults({});
  }
}

/**
 * Dashboard shell layout — sidebar + header + scrollable main.
 *
 * Covers all routes under (dashboard)/: the root dashboard page, agents,
 * events, credentials, policy, identity, storage, and settings.
 *
 * `DashboardSidebar` is a client component (needs usePathname for active
 * state). `DashboardHeader` is a client component (needs sidebar toggle state).
 * Both are passed as slots to Terra's `PageLayout` which handles the fixed-chrome,
 * scrollable-main structure at h-screen.
 *
 * On mobile (< md / 768 px), the fixed sidebar is hidden and replaced by a
 * slide-in `MobileSidebarDrawer` opened by the hamburger in `DashboardHeader`.
 *
 * `PermissionsProvider` fetches the principal's scope set from the BFF session
 * and provides it to all client components via React context (docs/adr/2026-04-14-frontend-policy-projection.md).
 */
export default async function DashboardLayout({ children }: { children: React.ReactNode }) {
  const session = await getSession();
  const initialPreferences = isSessionValid(session)
    ? await fetchPreferences(session.accessToken)
    : mergeWithDefaults({});

  return (
    <PermissionsProvider>
      <SidebarStateProvider>
        <PreferencesProvider initialPreferences={initialPreferences}>
          <PageLayout
            sidebarHeader={<SidebarBrandHeader />}
            sidebar={<DashboardSidebar />}
            sidebarFooter={<SidebarUserFooter />}
            header={<DashboardHeader />}
            mobileDrawer={<MobileSidebarDrawer />}
          >
            {children}
          </PageLayout>
        </PreferencesProvider>
      </SidebarStateProvider>
    </PermissionsProvider>
  );
}
